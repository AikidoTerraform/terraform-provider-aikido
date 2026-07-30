package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/AikidoSec/terraform-provider-aikido/internal/client"
	"github.com/AikidoSec/terraform-provider-aikido/internal/helpers"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func testPlannedDependency() dependencyModel {
	return dependencyModel{
		Enabled:                  types.BoolValue(true),
		SeverityFilter:           types.StringValue("critical_and_high_only"),
		ReposScope:               types.StringValue("selected"),
		RepoIDs:                  []int64{10, 20},
		UseAikidoLibraryForMajor: types.BoolValue(true),
	}
}

func TestConstructDependencyBody_Enabled(t *testing.T) {
	planned := testPlannedDependency()
	body := constructDependencyBody(&planned)
	want := map[string]any{
		"enabled":                      true,
		"severity_filter":              "critical_and_high_only",
		"repos_scope":                  "selected",
		"repo_ids":                     []int64{10, 20},
		"use_aikido_library_for_major": true,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructDependencyBody_Disabled(t *testing.T) {
	planned := testPlannedDependency()
	planned.Enabled = types.BoolValue(false)
	body := constructDependencyBody(&planned)
	want := map[string]any{"enabled": false}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestGetDependencySettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != dependencySettingsPath {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{
			"settings": {
				"enabled": true,
				"severity_filter": "upgrade_all_packages",
				"repos_scope": "all",
				"repo_ids": [],
				"use_aikido_library_for_major": false
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	dep, err := getDependencySettings(context.Background(), client.New(srv.Client(), srv.URL))
	if err != nil {
		t.Fatalf("getDependencySettings: %v", err)
	}
	if !dep.Enabled || dep.SeverityFilter != "upgrade_all_packages" || dep.ReposScope != "all" {
		t.Errorf("unexpected dependency %#v", dep)
	}
}

func TestMapDependencyAPIToModel(t *testing.T) {
	dep := mapDependencyAPIToModel(dependencySettingsAPI{
		Enabled:                  true,
		SeverityFilter:           "critical_and_high_only",
		ReposScope:               "selected",
		RepoIDs:                  []int64{7},
		UseAikidoLibraryForMajor: true,
	})
	if !dep.Enabled.ValueBool() || dep.SeverityFilter.ValueString() != "critical_and_high_only" {
		t.Errorf("unexpected dependency %#v", dep)
	}
}

func TestMergeDependencyPreservesIgnoredFields(t *testing.T) {
	prior := testPlannedDependency()
	prior.Enabled = types.BoolValue(false)

	dep := mergeDependencyAPIAndPrior(dependencySettingsAPI{
		Enabled:                  false,
		SeverityFilter:           "none",
		ReposScope:               "all",
		RepoIDs:                  []int64{},
		UseAikidoLibraryForMajor: false,
	}, &prior)
	if dep.SeverityFilter.ValueString() != "critical_and_high_only" {
		t.Errorf("dependency.severity_filter = %s", dep.SeverityFilter.ValueString())
	}
	if !reflect.DeepEqual(dep.RepoIDs, []int64{10, 20}) {
		t.Errorf("dependency.repo_ids = %#v", dep.RepoIDs)
	}
}

func TestDependencyApplySettings_EchoesPlannedWhenAPIRewrites(t *testing.T) {
	putCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if r.URL.Path != dependencySettingsPath {
				t.Errorf("unexpected PUT path %s", r.URL.Path)
			}
			putCount++
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": false,
					"severity_filter": "none",
					"repos_scope": "all",
					"repo_ids": [],
					"use_aikido_library_for_major": false
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlannedDependency()
	planned.Enabled = types.BoolValue(false)

	res := &autofixDependencySettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}

	if putCount != 1 {
		t.Errorf("PUT count = %d, want 1", putCount)
	}
	if state.Enabled.ValueBool() {
		t.Error("enabled = true, want false")
	}
	if state.SeverityFilter.ValueString() != "critical_and_high_only" {
		t.Errorf("severity_filter = %s", state.SeverityFilter.ValueString())
	}
	if state.ID.ValueString() != autofixDependencySettingsResourceID {
		t.Errorf("id = %s, want %s", state.ID.ValueString(), autofixDependencySettingsResourceID)
	}
}

func TestDependencyApplySettings_PUTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == dependencySettingsPath {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "invalid settings")
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	res := &autofixDependencySettingsResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applySettings(context.Background(), testPlannedDependency())
	if !diags.HasError() {
		t.Fatal("expected diagnostics error on PUT failure")
	}
}

func TestDependencyDelete_DisablesFeature(t *testing.T) {
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != dependencySettingsPath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
			t.Errorf("decoding PUT body: %v", err)
		}
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	res := &autofixDependencySettingsResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	diags := state.Set(ctx, testPlannedDependency())
	if diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}

	var resp resource.DeleteResponse
	res.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}

	if putBody["enabled"] != false {
		t.Errorf("enabled = %#v, want false", putBody["enabled"])
	}
	if len(putBody) != 1 {
		t.Errorf("body = %#v, want only enabled=false", putBody)
	}
}

func TestDependencyDelete_IgnoresNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	res := &autofixDependencySettingsResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	diags := state.Set(ctx, testPlannedDependency())
	if diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}

	var resp resource.DeleteResponse
	res.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete should ignore 404, got: %v", resp.Diagnostics)
	}
}

func intSet(ids ...int64) types.Set {
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.Int64Value(id))
	}
	return types.SetValueMust(types.Int64Type, elems)
}

func TestValidateDependencyConfig(t *testing.T) {
	tests := []struct {
		name      string
		enabled   types.Bool
		useMajor  types.Bool
		severity  types.String
		scope     types.String
		repoIDs   types.Set
		wantError bool
	}{
		{
			name:     "disabled allows all omitted",
			enabled:  types.BoolValue(false),
			useMajor: types.BoolNull(),
			severity: types.StringNull(),
			scope:    types.StringNull(),
			repoIDs:  types.SetNull(types.Int64Type),
		},
		{
			name:     "enabled unknown is skipped",
			enabled:  types.BoolUnknown(),
			useMajor: types.BoolNull(),
			severity: types.StringNull(),
			scope:    types.StringNull(),
			repoIDs:  types.SetNull(types.Int64Type),
		},
		{
			name:      "enabled requires severity_filter",
			enabled:   types.BoolValue(true),
			useMajor:  types.BoolValue(true),
			severity:  types.StringNull(),
			scope:     types.StringValue("all"),
			repoIDs:   types.SetNull(types.Int64Type),
			wantError: true,
		},
		{
			name:      "enabled requires repos_scope",
			enabled:   types.BoolValue(true),
			useMajor:  types.BoolValue(true),
			severity:  types.StringValue("critical_and_high_only"),
			scope:     types.StringNull(),
			repoIDs:   types.SetNull(types.Int64Type),
			wantError: true,
		},
		{
			name:      "enabled requires use_aikido_library_for_major",
			enabled:   types.BoolValue(true),
			useMajor:  types.BoolNull(),
			severity:  types.StringValue("critical_and_high_only"),
			scope:     types.StringValue("all"),
			repoIDs:   types.SetNull(types.Int64Type),
			wantError: true,
		},
		{
			name:      "selected scope requires non-empty repo_ids",
			enabled:   types.BoolValue(true),
			useMajor:  types.BoolValue(true),
			severity:  types.StringValue("critical_and_high_only"),
			scope:     types.StringValue("selected"),
			repoIDs:   intSet(),
			wantError: true,
		},
		{
			name:     "selected scope with repo_ids is valid",
			enabled:  types.BoolValue(true),
			useMajor: types.BoolValue(true),
			severity: types.StringValue("critical_and_high_only"),
			scope:    types.StringValue("selected"),
			repoIDs:  intSet(10, 20),
		},
		{
			name:     "all scope needs no repo_ids",
			enabled:  types.BoolValue(true),
			useMajor: types.BoolValue(true),
			severity: types.StringValue("critical_and_high_only"),
			scope:    types.StringValue("all"),
			repoIDs:  types.SetNull(types.Int64Type),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateDependencyConfig(tc.enabled, tc.useMajor, tc.severity, tc.scope, tc.repoIDs)
			if diags.HasError() != tc.wantError {
				t.Errorf("HasError() = %v, want %v (diags: %v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestMergeDependency_DisabledMirrorsNullPrior(t *testing.T) {
	prior := &dependencyModel{
		Enabled:                  types.BoolValue(false),
		SeverityFilter:           types.StringNull(),
		ReposScope:               types.StringNull(),
		RepoIDs:                  nil,
		UseAikidoLibraryForMajor: types.BoolNull(),
	}

	dep := mergeDependencyAPIAndPrior(dependencySettingsAPI{
		Enabled:                  false,
		SeverityFilter:           "none",
		ReposScope:               "all",
		RepoIDs:                  []int64{},
		UseAikidoLibraryForMajor: false,
	}, prior)

	if !dep.SeverityFilter.IsNull() {
		t.Errorf("severity_filter = %#v, want null (mirror prior)", dep.SeverityFilter)
	}
	if !dep.ReposScope.IsNull() {
		t.Errorf("repos_scope = %#v, want null (mirror prior)", dep.ReposScope)
	}
	if dep.RepoIDs != nil {
		t.Errorf("repo_ids = %#v, want nil (mirror prior)", dep.RepoIDs)
	}
	if !dep.UseAikidoLibraryForMajor.IsNull() {
		t.Errorf("use_aikido_library_for_major = %#v, want null (mirror prior)", dep.UseAikidoLibraryForMajor)
	}
}

func TestDroppedRepoIDs(t *testing.T) {
	tests := []struct {
		name    string
		planned []int64
		actual  []int64
		want    []int64
	}{
		{name: "none dropped", planned: []int64{10, 20}, actual: []int64{10, 20}, want: nil},
		{name: "one dropped", planned: []int64{10, 20, 30}, actual: []int64{10, 20}, want: []int64{30}},
		{name: "dedupes and sorts", planned: []int64{30, 10, 30, 20}, actual: []int64{10}, want: []int64{20, 30}},
		{name: "all dropped", planned: []int64{5, 6}, actual: []int64{}, want: []int64{5, 6}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := helpers.DroppedRepoIDs(tc.planned, tc.actual)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DroppedRepoIDs = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDependencyApplySettings_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"severity_filter": "critical_and_high_only",
					"repos_scope": "selected",
					"repo_ids": [10, 20],
					"use_aikido_library_for_major": true
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	res := &autofixDependencySettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), testPlannedDependency())
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}
	if !reflect.DeepEqual(state.RepoIDs, []int64{10, 20}) {
		t.Errorf("repo_ids = %#v, want [10 20]", state.RepoIDs)
	}
	if state.SeverityFilter.ValueString() != "critical_and_high_only" {
		t.Errorf("severity_filter = %s", state.SeverityFilter.ValueString())
	}
	if state.ID.ValueString() != autofixDependencySettingsResourceID {
		t.Errorf("id = %s, want %s", state.ID.ValueString(), autofixDependencySettingsResourceID)
	}
}

// When the API drops requested repo IDs (enabled+selected), applySettings must fail
// with an actionable error rather than silently accepting the filtered set.
func TestDependencyApplySettings_ErrorsOnDroppedRepoIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			// Requested 30 was dropped.
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"severity_filter": "critical_and_high_only",
					"repos_scope": "selected",
					"repo_ids": [10, 20],
					"use_aikido_library_for_major": true
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlannedDependency()
	planned.RepoIDs = []int64{10, 20, 30}

	res := &autofixDependencySettingsResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applySettings(context.Background(), planned)
	if !diags.HasError() {
		t.Fatal("expected error when the API drops repo IDs")
	}

	var mentioned bool
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), "30") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("error should name the dropped ID 30, got: %v", diags)
	}
}

func TestNormalizeIDs_NilIsEmpty(t *testing.T) {
	ids := helpers.NormalizeIDs(nil)
	if ids == nil {
		t.Fatal("want non-nil empty slice")
	}
	if len(ids) != 0 {
		t.Errorf("ids = %#v, want empty", ids)
	}
}
