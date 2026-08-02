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
)

func TestSetActive_Activate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"success":1,"was_already_activated":0}`)
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	if err := res.setActive(context.Background(), "42", true); err != nil {
		t.Fatalf("setActive: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/public/v1/repositories/code/activate" {
		t.Errorf("path = %s, want activate", gotPath)
	}
	if gotBody["code_repo_id"] != 42 {
		t.Errorf("body = %#v, want code_repo_id 42", gotBody)
	}
}

func TestSetActive_Deactivate(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"success":1,"was_already_deactivated":0}`)
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	if err := res.setActive(context.Background(), "7", false); err != nil {
		t.Fatalf("setActive: %v", err)
	}
	if gotPath != "/public/v1/repositories/code/deactivate" {
		t.Errorf("path = %s, want deactivate", gotPath)
	}
}

func TestSetActive_InvalidID(t *testing.T) {
	res := &repositoryResource{client: client.New(http.DefaultClient, "http://example.invalid")}
	if err := res.setActive(context.Background(), "not-a-number", true); err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestConfigure_UpdatesConfig(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if r.Method == http.MethodGet {
			if err := json.NewEncoder(w).Encode(repositoryAPI{ID: 9, Active: true}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := repositoryModel{
		ID:           types.StringValue("9"),
		Active:       types.BoolValue(true),
		Sensitivity:  types.StringValue("sensitive"),
		Connectivity: types.StringValue("connected"),
	}
	if _, err := res.setRepoConfig(context.Background(), plan); err != nil {
		t.Fatalf("configure: %v", err)
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
}

func TestConfigure_SkipsNullConfig(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if r.Method == http.MethodGet {
			if err := json.NewEncoder(w).Encode(repositoryAPI{ID: 9, Active: false}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := repositoryModel{
		ID:           types.StringValue("9"),
		Active:       types.BoolValue(false),
		Sensitivity:  types.StringNull(),
		Connectivity: types.StringNull(),
	}
	if _, err := res.setRepoConfig(context.Background(), plan); err != nil {
		t.Fatalf("configure: %v", err)
	}

	if calls["/public/v1/repositories/code/deactivate"] != 1 {
		t.Errorf("expected deactivate call, got %#v", calls)
	}
	if calls["/public/v1/repositories/code/9/sensitivity"] != 0 {
		t.Error("did not expect sensitivity update when null")
	}
	if calls["/public/v1/repositories/code/9/connectivity"] != 0 {
		t.Error("did not expect connectivity update when null")
	}
}

func TestRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/public/v1/repositories/code/1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(repositoryAPI{
			ID:             1,
			Name:           "Compression service",
			Provider:       "github",
			ExternalRepoID: "R_kgDOI5RlKA",
			Active:         true,
			Branch:         "main",
			URL:            "https://github.com/example/repo",
			Connectivity:   "connected",
			Sensitivity:    "normal",
		}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	apiRepository, err := res.getRepositoryDetails(context.Background(), "1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	state := repositoryModelFromAPI(apiRepository)
	if state.ID.ValueString() != "1" || !state.Active.ValueBool() {
		t.Errorf("unexpected state id/active: %+v", state)
	}
	if state.Name.ValueString() != "Compression service" {
		t.Errorf("name = %s", state.Name.ValueString())
	}
	if state.Sensitivity.ValueString() != "normal" || state.Connectivity.ValueString() != "connected" {
		t.Errorf("sensitivity/connectivity = %s/%s", state.Sensitivity.ValueString(), state.Connectivity.ValueString())
	}
	if state.Labels == nil || len(state.Labels) != 0 {
		t.Errorf("labels = %+v, want empty non-nil slice", state.Labels)
	}
}
