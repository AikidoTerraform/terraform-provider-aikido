package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/time/rate"
)

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

func writeReposList(t *testing.T, w http.ResponseWriter, repos ...repositoryAPI) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(repos); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

func isCodeReposList(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code"
}

func TestSetActive_ActivateAndDeactivate(t *testing.T) {
	t.Run("activate", func(t *testing.T) {
		var method, path string
		var body map[string]int64

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method = r.Method
			path = r.URL.Path
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = io.WriteString(w, `{"success":1}`)
		}))
		t.Cleanup(srv.Close)

		res := &repositoryResource{client: testClient(srv)}
		if err := res.setActive(context.Background(), "42", true); err != nil {
			t.Fatalf("setActive: %v", err)
		}

		if method != http.MethodPost || path != "/public/v1/repositories/code/activate" {
			t.Errorf("%s %s, want POST .../activate", method, path)
		}
		if body["code_repo_id"] != 42 {
			t.Errorf("body = %#v", body)
		}
	})

	t.Run("deactivate", func(t *testing.T) {
		var path string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			_, _ = io.WriteString(w, `{"success":1}`)
		}))
		t.Cleanup(srv.Close)

		res := &repositoryResource{client: testClient(srv)}
		if err := res.setActive(context.Background(), "7", false); err != nil {
			t.Fatalf("setActive: %v", err)
		}

		if path != "/public/v1/repositories/code/deactivate" {
			t.Errorf("path = %s, want deactivate", path)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		res := &repositoryResource{
			client: client.New(http.DefaultClient, "http://example.invalid", client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1))),
		}
		if err := res.setActive(context.Background(), "not-a-number", true); err == nil {
			t.Fatal("expected error for invalid id")
		}
	})
}

func TestSetRepoConfig_UpdatesAndSkipsNulls(t *testing.T) {
	t.Run("writes sensitivity and connectivity then detail GET", func(t *testing.T) {
		calls := map[string]int{}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[r.URL.Path]++

			if r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/9" {
				_ = json.NewEncoder(w).Encode(repositoryAPI{ID: 9, Active: true, Name: "from-detail"})
				return
			}

			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)

		res := &repositoryResource{client: testClient(srv)}
		state, err := res.setRepoConfig(context.Background(), repositoryModel{
			ID:           types.StringValue("9"),
			Active:       types.BoolValue(true),
			Sensitivity:  types.StringValue("sensitive"),
			Connectivity: types.StringValue("connected"),
		})
		if err != nil {
			t.Fatalf("setRepoConfig: %v", err)
		}

		want := map[string]int{
			"/public/v1/repositories/code/activate":       1,
			"/public/v1/repositories/code/9/sensitivity":  1,
			"/public/v1/repositories/code/9/connectivity": 1,
			"/public/v1/repositories/code/9":              1,
		}
		for path, n := range want {
			if calls[path] != n {
				t.Errorf("calls[%s] = %d, want %d", path, calls[path], n)
			}
		}
		if calls["/public/v1/repositories/code"] != 0 {
			t.Errorf("configure must not list repos: %#v", calls)
		}
		if state.Name.ValueString() != "from-detail" {
			t.Errorf("name = %q, want from-detail", state.Name.ValueString())
		}
	})

	t.Run("skips null sensitivity and connectivity", func(t *testing.T) {
		calls := map[string]int{}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[r.URL.Path]++

			if r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/9" {
				_ = json.NewEncoder(w).Encode(repositoryAPI{ID: 9, Active: false})
				return
			}

			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)

		res := &repositoryResource{client: testClient(srv)}
		if _, err := res.setRepoConfig(context.Background(), repositoryModel{
			ID:           types.StringValue("9"),
			Active:       types.BoolValue(false),
			Sensitivity:  types.StringNull(),
			Connectivity: types.StringNull(),
		}); err != nil {
			t.Fatalf("setRepoConfig: %v", err)
		}

		if calls["/public/v1/repositories/code/deactivate"] != 1 {
			t.Errorf("expected deactivate, got %#v", calls)
		}
		if calls["/public/v1/repositories/code/9/sensitivity"] != 0 || calls["/public/v1/repositories/code/9/connectivity"] != 0 {
			t.Errorf("unexpected config PUTs: %#v", calls)
		}
	})
}

func TestRepositoryFromCache_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isCodeReposList(r) {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeReposList(t, w)
	}))
	t.Cleanup(srv.Close)

	_, err := repositoryFromCache(context.Background(), testClient(srv), 1)
	if err == nil || !client.NotFound(err) {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestRepositoryModelFromAPI(t *testing.T) {
	state := repositoryModelFromAPI(repositoryAPI{
		ID:             1,
		Name:           "Compression service",
		Provider:       "github",
		ExternalRepoID: "R_kgDOI5RlKA",
		Active:         true,
		Branch:         "main",
		URL:            "https://github.com/example/repo",
		Connectivity:   "connected",
		Sensitivity:    "normal",
		Labels:         []labelAPI{{ID: "10", Name: "payments"}},
	})

	if state.ID.ValueString() != "1" || !state.Active.ValueBool() {
		t.Errorf("id/active = %s/%v", state.ID.ValueString(), state.Active.ValueBool())
	}
	if state.Name.ValueString() != "Compression service" {
		t.Errorf("name = %s", state.Name.ValueString())
	}
	if state.Sensitivity.ValueString() != "normal" || state.Connectivity.ValueString() != "connected" {
		t.Errorf("sensitivity/connectivity = %s/%s", state.Sensitivity.ValueString(), state.Connectivity.ValueString())
	}
	if len(state.Labels) != 1 || state.Labels[0].ValueString() != "payments" {
		t.Errorf("labels = %+v", state.Labels)
	}

	emptyLabels := labelNamesFromAPI(nil)
	if emptyLabels == nil || len(emptyLabels) != 0 {
		t.Errorf("labelNamesFromAPI(nil) = %+v, want empty non-nil", emptyLabels)
	}
}
