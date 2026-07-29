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

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func testPlannedSast() sastModel {
	return sastModel{
		Enabled:        types.BoolValue(true),
		SeverityFilter: types.StringValue("critical_issues_only"),
		ReposScope:     types.StringValue("selected"),
		RepoIDs:        []int64{30, 40},
	}
}

func TestConstructSastBody_Enabled(t *testing.T) {
	planned := testPlannedSast()
	body := constructSastBody(&planned)
	want := map[string]any{
		"enabled":         true,
		"severity_filter": "critical_issues_only",
		"repos_scope":     "selected",
		"repo_ids":        []int64{30, 40},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructSastBody_Disabled(t *testing.T) {
	planned := testPlannedSast()
	planned.Enabled = types.BoolValue(false)
	body := constructSastBody(&planned)
	want := map[string]any{"enabled": false}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestGetSastSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sastSettingsPath {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{
			"settings": {
				"enabled": true,
				"severity_filter": "all",
				"repos_scope": "selected",
				"repo_ids": [9]
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	sast, err := getSastSettings(context.Background(), client.New(srv.Client(), srv.URL))
	if err != nil {
		t.Fatalf("getSastSettings: %v", err)
	}
	if !sast.Enabled || sast.SeverityFilter != "all" || !reflect.DeepEqual(sast.RepoIDs, []int64{9}) {
		t.Errorf("unexpected sast %#v", sast)
	}
}

func TestMapSastAPIToModel(t *testing.T) {
	sast := mapSastAPIToModel(sastSettingsAPI{
		Enabled:        false,
		SeverityFilter: "none",
		ReposScope:     "none",
		RepoIDs:        []int64{},
	})
	if sast.Enabled.ValueBool() || sast.SeverityFilter.ValueString() != "none" {
		t.Errorf("unexpected sast %#v", sast)
	}
}

func TestMergeSastPreservesIgnoredFields(t *testing.T) {
	prior := testPlannedSast()
	prior.Enabled = types.BoolValue(false)

	sast := mergeSastAPIAndPrior(sastSettingsAPI{
		Enabled:        false,
		SeverityFilter: "none",
		ReposScope:     "none",
		RepoIDs:        []int64{},
	}, &prior)
	if sast.SeverityFilter.ValueString() != "critical_issues_only" {
		t.Errorf("sast.severity_filter = %s", sast.SeverityFilter.ValueString())
	}
	if !reflect.DeepEqual(sast.RepoIDs, []int64{30, 40}) {
		t.Errorf("sast.repo_ids = %#v", sast.RepoIDs)
	}
}

func TestSastApplySettings_EchoesPlannedWhenAPIRewrites(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": false,
					"severity_filter": "none",
					"repos_scope": "none",
					"repo_ids": []
				}
			}`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlannedSast()
	planned.Enabled = types.BoolValue(false)

	res := &autofixSastSettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}

	if state.ReposScope.ValueString() != "selected" {
		t.Errorf("repos_scope = %s", state.ReposScope.ValueString())
	}
	if state.ID.ValueString() != autofixSastSettingsResourceID {
		t.Errorf("id = %s, want %s", state.ID.ValueString(), autofixSastSettingsResourceID)
	}
}

func TestSastDelete_DisablesFeature(t *testing.T) {
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != sastSettingsPath {
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
	res := &autofixSastSettingsResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	diags := state.Set(ctx, testPlannedSast())
	if diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}

	var resp resource.DeleteResponse
	res.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}

	if putBody["enabled"] != false || len(putBody) != 1 {
		t.Errorf("body = %#v, want only enabled=false", putBody)
	}
}

func TestNormalizeIDs_NilIsEmpty(t *testing.T) {
	ids := normalizeIDs(nil)
	if ids == nil {
		t.Fatal("want non-nil empty slice")
	}
	if len(ids) != 0 {
		t.Errorf("ids = %#v, want empty", ids)
	}
}

func intSet(ids ...int64) types.Set {
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.Int64Value(id))
	}
	return types.SetValueMust(types.Int64Type, elems)
}

func TestValidateSastConfig(t *testing.T) {
	tests := []struct {
		name      string
		enabled   types.Bool
		severity  types.String
		scope     types.String
		repoIDs   types.Set
		wantError bool
	}{
		{
			name:     "disabled allows all omitted",
			enabled:  types.BoolValue(false),
			severity: types.StringNull(),
			scope:    types.StringNull(),
			repoIDs:  types.SetNull(types.Int64Type),
		},
		{
			name:     "enabled unknown is skipped",
			enabled:  types.BoolUnknown(),
			severity: types.StringNull(),
			scope:    types.StringNull(),
			repoIDs:  types.SetNull(types.Int64Type),
		},
		{
			name:      "enabled requires severity_filter",
			enabled:   types.BoolValue(true),
			severity:  types.StringNull(),
			scope:     types.StringValue("all"),
			repoIDs:   types.SetNull(types.Int64Type),
			wantError: true,
		},
		{
			name:      "enabled requires repos_scope",
			enabled:   types.BoolValue(true),
			severity:  types.StringValue("critical_and_high_only"),
			scope:     types.StringNull(),
			repoIDs:   types.SetNull(types.Int64Type),
			wantError: true,
		},
		{
			name:      "selected scope requires non-empty repo_ids",
			enabled:   types.BoolValue(true),
			severity:  types.StringValue("critical_and_high_only"),
			scope:     types.StringValue("selected"),
			repoIDs:   intSet(),
			wantError: true,
		},
		{
			name:     "selected scope with repo_ids is valid",
			enabled:  types.BoolValue(true),
			severity: types.StringValue("critical_and_high_only"),
			scope:    types.StringValue("selected"),
			repoIDs:  intSet(10, 20),
		},
		{
			name:     "all scope needs no repo_ids",
			enabled:  types.BoolValue(true),
			severity: types.StringValue("critical_and_high_only"),
			scope:    types.StringValue("all"),
			repoIDs:  types.SetNull(types.Int64Type),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateSastConfig(tc.enabled, tc.severity, tc.scope, tc.repoIDs)
			if diags.HasError() != tc.wantError {
				t.Errorf("HasError() = %v, want %v (diags: %v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestMergeSast_DisabledMirrorsNullPrior(t *testing.T) {
	prior := &sastModel{
		Enabled:        types.BoolValue(false),
		SeverityFilter: types.StringNull(),
		ReposScope:     types.StringNull(),
		RepoIDs:        nil,
	}

	sast := mergeSastAPIAndPrior(sastSettingsAPI{
		Enabled:        false,
		SeverityFilter: "none",
		ReposScope:     "all",
		RepoIDs:        []int64{},
	}, prior)

	if !sast.SeverityFilter.IsNull() {
		t.Errorf("severity_filter = %#v, want null (mirror prior)", sast.SeverityFilter)
	}
	if !sast.ReposScope.IsNull() {
		t.Errorf("repos_scope = %#v, want null (mirror prior)", sast.ReposScope)
	}
	if sast.RepoIDs != nil {
		t.Errorf("repo_ids = %#v, want nil (mirror prior)", sast.RepoIDs)
	}
}

func TestDroppedSastRepoIDs(t *testing.T) {
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
			got := droppedRepoIDs(tc.planned, tc.actual)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("droppedRepoIDs = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSastApplySettings_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"severity_filter": "critical_issues_only",
					"repos_scope": "selected",
					"repo_ids": [30, 40]
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	res := &autofixSastSettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), testPlannedSast())
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}
	if !reflect.DeepEqual(state.RepoIDs, []int64{30, 40}) {
		t.Errorf("repo_ids = %#v, want [30 40]", state.RepoIDs)
	}
	if state.SeverityFilter.ValueString() != "critical_issues_only" {
		t.Errorf("severity_filter = %s", state.SeverityFilter.ValueString())
	}
	if state.ID.ValueString() != autofixSastSettingsResourceID {
		t.Errorf("id = %s, want %s", state.ID.ValueString(), autofixSastSettingsResourceID)
	}
}

// When the API drops requested repo IDs (enabled+selected), applySettings must fail
// with an actionable error rather than silently accepting the filtered set.
func TestSastApplySettings_ErrorsOnDroppedRepoIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			// Requested 30 was dropped.
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"severity_filter": "critical_issues_only",
					"repos_scope": "selected",
					"repo_ids": [10, 20]
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlannedSast()
	planned.RepoIDs = []int64{10, 20, 30}

	res := &autofixSastSettingsResource{client: client.New(srv.Client(), srv.URL)}
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
