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

	"github.com/AikidoSec/terraform-provider-aikido/internal/client"
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

func TestConfigure_CreatesLabels(t *testing.T) {
	var created []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			var body map[string]string
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding create body: %v", err)
			}
			created = append(created, body["name"])
			id := 100 + len(created)
			_, _ = io.WriteString(w, `{"label_id":`+strconv.Itoa(id)+`}`)
		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
			if err := json.NewEncoder(w).Encode(repositoryAPI{ID: 1, Active: true}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: labelSet("payments", "production"),
	}
	state, err := res.setRepoConfig(context.Background(), plan)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(created) != 2 || created[0] != "payments" || created[1] != "production" {
		t.Errorf("created = %#v", created)
	}
	if got := labelNames(state.Labels); len(got) != 2 || got[0] != "payments" || got[1] != "production" {
		t.Errorf("state labels = %#v", got)
	}
}

func TestConfigure_DeletesRemovedLabels(t *testing.T) {
	var deleted []string
	var created int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
			deleted = append(deleted, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			created++
			_, _ = io.WriteString(w, `{"label_id":99}`)
		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
			if err := json.NewEncoder(w).Encode(repositoryAPI{
				ID:     1,
				Active: true,
				Labels: []labelAPI{
					{ID: 10, Name: "payments"},
					{ID: 11, Name: "remove-me"},
				},
			}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: labelSet("payments"),
	}
	state, err := res.setRepoConfig(context.Background(), plan)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/11" {
		t.Errorf("deleted = %v", deleted)
	}
	if created != 0 {
		t.Errorf("unexpected creates: %d", created)
	}
	if got := labelNames(state.Labels); len(got) != 1 || got[0] != "payments" {
		t.Errorf("state labels = %#v", got)
	}
}

func TestConfigure_LeavesLabelsWhenOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
			if err := json.NewEncoder(w).Encode(repositoryAPI{
				ID:     1,
				Active: true,
				Labels: []labelAPI{{ID: 10, Name: "production"}},
			}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: nil,
	}
	state, err := res.setRepoConfig(context.Background(), plan)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if state.Labels != nil {
		t.Errorf("state.Labels = %+v, want nil", state.Labels)
	}
}

func TestConfigure_ResetsLabelsWhenEmptyArray(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
			deleted = append(deleted, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/repositories/code/1":
			if err := json.NewEncoder(w).Encode(repositoryAPI{
				ID:     1,
				Active: true,
				Labels: []labelAPI{
					{ID: 10, Name: "payments"},
					{ID: 11, Name: "production"},
				},
			}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: []types.String{},
	}
	state, err := res.setRepoConfig(context.Background(), plan)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("deleted = %v, want both labels", deleted)
	}
	if state.Labels == nil || len(state.Labels) != 0 {
		t.Errorf("state.Labels = %+v, want empty non-nil slice", state.Labels)
	}
}

func TestApplyLabels_CreatesAndDeletesByName(t *testing.T) {
	var created, deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			var body map[string]string
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding create body: %v", err)
			}
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

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	current := []labelAPI{
		{ID: 10, Name: "payments", IsImported: false},
		{ID: 11, Name: "production", IsImported: false},
		{ID: 12, Name: "imported", IsImported: true},
	}
	// Keep production, add billing, drop payments; imported stays even though unplanned.
	plan := labelSet("production", "billing")
	if err := res.applyLabels(context.Background(), "1", plan, current); err != nil {
		t.Fatalf("applyLabels: %v", err)
	}
	if len(created) != 1 || created[0] != "billing" {
		t.Errorf("created = %#v, want billing", created)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/10" {
		t.Errorf("deleted = %v, want payments id 10", deleted)
	}
}

func TestApplyLabels_OmittedIsNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	current := []labelAPI{{ID: 10, Name: "payments"}}
	if err := res.applyLabels(context.Background(), "1", nil, current); err != nil {
		t.Fatalf("applyLabels: %v", err)
	}
}

func TestRead_RefreshesManagedLabelsFromAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/public/v1/repositories/code/1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(repositoryAPI{
			ID:     1,
			Active: true,
			Labels: []labelAPI{
				{ID: 10, Name: "payments"},
			},
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
	if got := labelNames(state.Labels); len(got) != 1 || got[0] != "payments" {
		t.Errorf("labels = %#v, want only payments", got)
	}
}

func TestLabelNamesFromAPI_EmptyIsNonNil(t *testing.T) {
	got := labelNamesFromAPI(nil)
	if got == nil {
		t.Fatal("want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}
