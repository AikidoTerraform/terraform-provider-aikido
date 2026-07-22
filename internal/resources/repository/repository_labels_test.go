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

func TestConfigure_UpdatesAndDeletesLabels(t *testing.T) {
	var updatedPath, deletedPath string
	var updatedBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels/10":
			updatedPath = r.URL.Path
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&updatedBody); err != nil {
				t.Errorf("decoding update body: %v", err)
			}
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/public/v1/repositories/code/1/labels/11":
			deletedPath = r.URL.Path
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
		{ID: types.StringValue("10"), Name: types.StringValue("old"), IsImported: types.BoolValue(false)},
		{ID: types.StringValue("11"), Name: types.StringValue("remove-me"), IsImported: types.BoolValue(false)},
	}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: []labelModel{
			{ID: types.StringValue("10"), Name: types.StringValue("renamed")},
		},
	}
	state, err := res.setRepoConfig(context.Background(), plan, prior)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if updatedPath != "/public/v1/repositories/code/1/labels/10" || updatedBody["name"] != "renamed" {
		t.Errorf("update path/body = %s %#v", updatedPath, updatedBody)
	}
	if deletedPath != "/public/v1/repositories/code/1/labels/11" {
		t.Errorf("deleted path = %s", deletedPath)
	}
	if len(state.Labels) != 1 || state.Labels[0].Name.ValueString() != "renamed" {
		t.Errorf("state labels = %+v", state.Labels)
	}
}

func TestSyncLabels_DoesNotDeleteUnknownAPILabels(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"label_id":42}`)
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	plan := []labelModel{{Name: types.StringValue("new")}}
	got, err := res.syncLabels(context.Background(), "1", plan, nil)
	if err != nil {
		t.Fatalf("syncLabels: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("unexpected deletes: %v", deleted)
	}
	if len(got) != 1 || got[0].ID.ValueString() != "42" {
		t.Errorf("got %+v", got)
	}
}

// Reproduces apply failure when config drops one label and keeps another by name:
// Terraform list index shifts, plan often has empty/wrong id, and a naive create
// POSTs the kept name again → API 400 "already exists".
func TestSyncLabels_KeepsByNameWhenListShrinks(t *testing.T) {
	var created, deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			created = append(created, r.URL.Path)
			t.Errorf("should not create label that already exists by name")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"status_code":400,"reason_phrase":"A label with this name already exists for this repository"}`)
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
		{ID: types.StringValue("178664"), Name: types.StringValue("payments"), IsImported: types.BoolValue(false)},
		{ID: types.StringValue("178665"), Name: types.StringValue("production"), IsImported: types.BoolValue(false)},
	}
	// Plan as Terraform often sends after shrinking: kept name at index 0, no id.
	plan := []labelModel{
		{Name: types.StringValue("production")},
	}
	got, err := res.syncLabels(context.Background(), "1", plan, prior)
	if err != nil {
		t.Fatalf("syncLabels: %v", err)
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

// Rename with unknown computed id (no UseStateForUnknown): reuse prior at same index.
func TestSyncLabels_RenamesUsingPriorIndexWhenIDUnknown(t *testing.T) {
	var updatedPath string
	var updatedBody map[string]string
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels/178664":
			updatedPath = r.URL.Path
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&updatedBody); err != nil {
				t.Errorf("decode: %v", err)
			}
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/1/labels":
			created = true
			t.Error("should rename existing label, not create")
			_, _ = io.WriteString(w, `{"label_id":999}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := &repositoryResource{client: client.New(srv.Client(), srv.URL)}
	prior := []labelModel{
		{ID: types.StringValue("178664"), Name: types.StringValue("old"), IsImported: types.BoolValue(false)},
	}
	plan := []labelModel{
		{Name: types.StringValue("renamed")}, // id unknown/empty after plan
	}
	got, err := res.syncLabels(context.Background(), "1", plan, prior)
	if err != nil {
		t.Fatalf("syncLabels: %v", err)
	}
	if created {
		t.Fatal("unexpected create")
	}
	if updatedPath != "/public/v1/repositories/code/1/labels/178664" || updatedBody["name"] != "renamed" {
		t.Errorf("update path/body = %s %#v", updatedPath, updatedBody)
	}
	if len(got) != 1 || got[0].ID.ValueString() != "178664" {
		t.Errorf("got %+v, want id 178664", got)
	}
}

// Removing the last managed label omits labels from the plan (nil), but prior
// still has it — must DELETE and clear state.
func TestConfigure_DeletesLastManagedLabelWhenOmitted(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/repositories/code/activate":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/public/v1/repositories/code/1/labels/178664":
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
		{ID: types.StringValue("178664"), Name: types.StringValue("production"), IsImported: types.BoolValue(false)},
	}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: nil, // attribute omitted after removing the last label
	}
	state, err := res.setRepoConfig(context.Background(), plan, prior)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "/public/v1/repositories/code/1/labels/178664" {
		t.Errorf("deleted = %v, want production label deleted", deleted)
	}
	if state.Labels != nil {
		t.Errorf("state.Labels = %+v, want nil", state.Labels)
	}
}

// labels = [] must delete previously managed labels and reset state to empty.
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
		{ID: types.StringValue("10"), Name: types.StringValue("payments"), IsImported: types.BoolValue(false)},
		{ID: types.StringValue("11"), Name: types.StringValue("production"), IsImported: types.BoolValue(false)},
	}
	plan := repositoryModel{
		ID:     types.StringValue("1"),
		Active: types.BoolValue(true),
		Labels: []labelModel{}, // explicit empty list
	}
	state, err := res.setRepoConfig(context.Background(), plan, prior)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("deleted = %v, want both managed labels", deleted)
	}
	if state.Labels == nil || len(state.Labels) != 0 {
		t.Errorf("state.Labels = %+v, want empty non-nil slice", state.Labels)
	}
}
