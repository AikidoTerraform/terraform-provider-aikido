package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func labelSet(names ...string) []types.String {
	labels := make([]types.String, 0, len(names))
	for _, name := range names {
		labels = append(labels, types.StringValue(name))
	}
	return labels
}

func labelNames(labels []types.String) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, label.ValueString())
	}
	return names
}

func TestSetRepoConfig_LabelLifecycle(t *testing.T) {
	t.Run("creates planned labels", func(t *testing.T) {
		var created []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
				_, _ = io.WriteString(w, `{}`)

			case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
				var body map[string]string
				defer r.Body.Close()
				_ = json.NewDecoder(r.Body).Decode(&body)
				created = append(created, body["name"])
				_, _ = io.WriteString(w, `{"label_id":`+strconv.Itoa(100+len(created))+`}`)

			case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
				_ = json.NewEncoder(w).Encode(repositoryAPI{ID: 1, Active: true})

			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		state, err := (&repositoryResource{client: testClient(srv)}).setRepoConfig(context.Background(), repositoryModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
			Labels: labelSet("payments", "production"),
		})
		if err != nil {
			t.Fatalf("setRepoConfig: %v", err)
		}

		if len(created) != 2 || created[0] != "payments" || created[1] != "production" {
			t.Errorf("created = %#v", created)
		}
		if got := labelNames(state.Labels); len(got) != 2 {
			t.Errorf("state labels = %#v", got)
		}
	})

	t.Run("deletes removed labels", func(t *testing.T) {
		var deleted []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
				_, _ = io.WriteString(w, `{}`)

			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
				deleted = append(deleted, r.URL.Path)
				_, _ = io.WriteString(w, `{}`)

			case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
				_ = json.NewEncoder(w).Encode(repositoryAPI{
					ID:     1,
					Active: true,
					Labels: []labelAPI{
						{ID: "10", Name: "payments"},
						{ID: "11", Name: "remove-me"},
					},
				})

			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		state, err := (&repositoryResource{client: testClient(srv)}).setRepoConfig(context.Background(), repositoryModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
			Labels: labelSet("payments"),
		})
		if err != nil {
			t.Fatalf("setRepoConfig: %v", err)
		}

		if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/11" {
			t.Errorf("deleted = %v", deleted)
		}
		if got := labelNames(state.Labels); len(got) != 1 || got[0] != "payments" {
			t.Errorf("state labels = %#v", got)
		}
	})

	t.Run("omitted labels leave remote untouched", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
				_, _ = io.WriteString(w, `{}`)

			case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
				_ = json.NewEncoder(w).Encode(repositoryAPI{
					ID:     1,
					Active: true,
					Labels: []labelAPI{{ID: "10", Name: "production"}},
				})

			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		state, err := (&repositoryResource{client: testClient(srv)}).setRepoConfig(context.Background(), repositoryModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
			Labels: nil,
		})
		if err != nil {
			t.Fatalf("setRepoConfig: %v", err)
		}

		if state.Labels != nil {
			t.Errorf("state.Labels = %+v, want nil", state.Labels)
		}
	})

	t.Run("empty labels delete all unmanaged labels", func(t *testing.T) {
		var deleted []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
				_, _ = io.WriteString(w, `{}`)

			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
				deleted = append(deleted, r.URL.Path)
				_, _ = io.WriteString(w, `{}`)

			case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
				_ = json.NewEncoder(w).Encode(repositoryAPI{
					ID:     1,
					Active: true,
					Labels: []labelAPI{
						{ID: "10", Name: "payments"},
						{ID: "11", Name: "production"},
					},
				})

			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		state, err := (&repositoryResource{client: testClient(srv)}).setRepoConfig(context.Background(), repositoryModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
			Labels: []types.String{},
		})
		if err != nil {
			t.Fatalf("setRepoConfig: %v", err)
		}

		if len(deleted) != 2 {
			t.Errorf("deleted = %v, want both labels", deleted)
		}
		if state.Labels == nil || len(state.Labels) != 0 {
			t.Errorf("state.Labels = %+v, want empty non-nil", state.Labels)
		}
	})
}

func TestApplyLabels_KeepsImportedAndSyncsNames(t *testing.T) {
	var created, deleted []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			var body map[string]string
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body["name"])
			_, _ = io.WriteString(w, `{"label_id":12}`)

		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	current := []labelAPI{
		{ID: "10", Name: "payments", IsImported: false},
		{ID: "11", Name: "production", IsImported: false},
		{ID: "12", Name: "imported", IsImported: true},
	}

	err := (&repositoryResource{client: testClient(srv)}).applyLabels(
		context.Background(),
		"1",
		labelSet("production", "billing"),
		current,
	)
	if err != nil {
		t.Fatalf("applyLabels: %v", err)
	}

	if len(created) != 1 || created[0] != "billing" {
		t.Errorf("created = %#v, want billing", created)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/10" {
		t.Errorf("deleted = %v, want payments id 10 (imported must stay)", deleted)
	}

	// Omitted plan is a no-op (no HTTP).
	noopServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(noopServer.Close)

	if err := (&repositoryResource{client: testClient(noopServer)}).applyLabels(
		context.Background(),
		"1",
		nil,
		[]labelAPI{{ID: "10", Name: "payments"}},
	); err != nil {
		t.Fatalf("omitted applyLabels: %v", err)
	}
}
