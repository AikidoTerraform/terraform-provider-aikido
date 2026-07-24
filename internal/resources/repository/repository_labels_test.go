package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

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
		Labels: []labelModel{
			{Name: types.StringValue("payments")},
			{Name: types.StringValue("production")},
		},
	}
	state, err := res.setRepoConfig(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(created) != 2 || created[0] != "payments" || created[1] != "production" {
		t.Errorf("created = %#v", created)
	}
	if len(state.Labels) != 2 {
		t.Fatalf("state labels = %+v", state.Labels)
	}
	if state.Labels[0].ID.ValueString() == "" || state.Labels[1].ID.ValueString() == "" {
		t.Errorf("expected label ids in state: %+v", state.Labels)
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
	prior := []labelModel{
		{ID: types.StringValue("10"), Name: types.StringValue("payments")},
		{ID: types.StringValue("11"), Name: types.StringValue("remove-me")},
	}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: []labelModel{
			{ID: types.StringValue("10"), Name: types.StringValue("payments")},
		},
	}
	state, err := res.setRepoConfig(context.Background(), plan, prior)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/11" {
		t.Errorf("deleted = %v", deleted)
	}
	if created != 0 {
		t.Errorf("unexpected creates: %d", created)
	}
	if len(state.Labels) != 1 || state.Labels[0].ID.ValueString() != "10" {
		t.Errorf("state labels = %+v", state.Labels)
	}
}

func TestApplyLabels_KeepsByIDWhenListShrinks(t *testing.T) {
	var created, deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			created = append(created, r.URL.Path)
			t.Errorf("should not create label that already exists by id")
			w.WriteHeader(http.StatusBadRequest)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
			t.Errorf("should not update unchanged label: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
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
	prior := []labelModel{
		{ID: types.StringValue("178664"), Name: types.StringValue("payments")},
		{ID: types.StringValue("178665"), Name: types.StringValue("production")},
	}
	plan := []labelModel{
		{ID: types.StringValue("178665"), Name: types.StringValue("production")},
	}
	got, err := res.applyLabels(context.Background(), "1", plan, prior)
	if err != nil {
		t.Fatalf("applyLabels: %v", err)
	}
	if len(created) != 0 {
		t.Errorf("unexpected creates: %v", created)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/178664" {
		t.Errorf("deleted = %v, want payments id 178664", deleted)
	}
	if len(got) != 1 || got[0].ID.ValueString() != "178665" {
		t.Errorf("got %+v", got)
	}
}

func TestApplyLabels_KeepsByNameWhenListShrinksAndIDIsMissing(t *testing.T) {
	var created, deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			created = append(created, r.URL.Path)
			t.Errorf("should not create label that already exists by name")
			w.WriteHeader(http.StatusBadRequest)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
			t.Errorf("should not update unchanged label: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
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
	prior := []labelModel{
		{ID: types.StringValue("178664"), Name: types.StringValue("payments")},
		{ID: types.StringValue("178665"), Name: types.StringValue("production")},
	}
	plan := []labelModel{
		{Name: types.StringValue("production")},
	}
	got, err := res.applyLabels(context.Background(), "1", plan, prior)
	if err != nil {
		t.Fatalf("applyLabels: %v", err)
	}
	if len(created) != 0 {
		t.Errorf("unexpected creates: %v", created)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/178664" {
		t.Errorf("deleted = %v, want payments id 178664", deleted)
	}
	if len(got) != 1 || got[0].ID.ValueString() != "178665" || got[0].Name.ValueString() != "production" {
		t.Errorf("got %+v", got)
	}
}

func TestConfigure_LeavesLabelsWhenOmitted(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
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
	prior := []labelModel{
		{ID: types.StringValue("178664"), Name: types.StringValue("production")},
	}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: nil,
	}
	state, err := res.setRepoConfig(context.Background(), plan, prior)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted = %v, want none", deleted)
	}
	if state.Labels != nil {
		t.Errorf("state.Labels = %+v, want nil", state.Labels)
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
	state, err := res.getRepositoryDetails(context.Background(), "1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(state.Labels) != 1 || state.Labels[0].ID.ValueString() != "10" || state.Labels[0].Name.ValueString() != "payments" {
		t.Errorf("labels = %+v, want only payments", state.Labels)
	}
}

func TestLabelModelsFromAPI_EmptyIsNonNil(t *testing.T) {
	got := labelModelsFromAPI(nil)
	if got == nil {
		t.Fatal("want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestApplyLabels_RecreatesMissingLabel(t *testing.T) {
	var created []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			var body map[string]string
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding create body: %v", err)
			}
			created = append(created, body["name"])
			_, _ = io.WriteString(w, `{"label_id":99}`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/public/v1/repositories/code/1/labels/"):
			t.Errorf("should not update unchanged label: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	prior := []labelModel{
		{ID: types.StringValue("10"), Name: types.StringValue("payments")},
	}
	plan := []labelModel{
		{ID: types.StringValue("10"), Name: types.StringValue("payments")},
		{Name: types.StringValue("production")},
	}
	got, err := res.applyLabels(context.Background(), "1", plan, prior)
	if err != nil {
		t.Fatalf("applyLabels: %v", err)
	}
	if len(created) != 1 || created[0] != "production" {
		t.Errorf("created = %#v, want production", created)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].ID.ValueString() != "10" || got[1].ID.ValueString() != "99" {
		t.Errorf("got %+v", got)
	}
}

func TestApplyLabels_UpdatesRenamedLabelByID(t *testing.T) {
	var updatedPath, updatedName string
	var created, deleted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels/10":
			var body map[string]string
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding update body: %v", err)
			}
			updatedPath = r.URL.Path
			updatedName = body["name"]
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			created++
			t.Errorf("should update in place, not create")
			w.WriteHeader(http.StatusBadRequest)
		case r.Method == http.MethodDelete:
			deleted++
			t.Errorf("should update in place, not delete: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	prior := []labelModel{
		{ID: types.StringValue("10"), Name: types.StringValue("payments")},
	}
	plan := []labelModel{
		{ID: types.StringValue("10"), Name: types.StringValue("billing")},
	}
	got, err := res.applyLabels(context.Background(), "1", plan, prior)
	if err != nil {
		t.Fatalf("applyLabels: %v", err)
	}
	if created != 0 || deleted != 0 {
		t.Errorf("created=%d deleted=%d, want no create/delete", created, deleted)
	}
	if updatedPath != "/public/v1/repositories/code/1/labels/10" || updatedName != "billing" {
		t.Errorf("updated path/name = %s/%q", updatedPath, updatedName)
	}
	if len(got) != 1 || got[0].ID.ValueString() != "10" || got[0].Name.ValueString() != "billing" {
		t.Errorf("got %+v", got)
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
	prior := []labelModel{
		{ID: types.StringValue("10"), Name: types.StringValue("payments")},
		{ID: types.StringValue("11"), Name: types.StringValue("production")},
	}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: []labelModel{},
	}
	state, err := res.setRepoConfig(context.Background(), plan, prior)
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
