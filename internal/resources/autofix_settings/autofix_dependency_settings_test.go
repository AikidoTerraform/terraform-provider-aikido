package autofix_settings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func testPlannedDependency() dependencyModel {
	return dependencyModel{
		Enabled:                  types.BoolValue(true),
		UpgradeType:              types.StringValue("critical_and_high_only"),
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
		"upgrade_type":                 "critical_and_high_only",
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
				"upgrade_type": "upgrade_all_packages",
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
	if !dep.Enabled || dep.UpgradeType != "upgrade_all_packages" || dep.ReposScope != "all" {
		t.Errorf("unexpected dependency %#v", dep)
	}
}

func TestMapDependencyAPIToModel(t *testing.T) {
	dep := mapDependencyAPIToModel(dependencySettingsAPI{
		Enabled:                  true,
		UpgradeType:              "critical_and_high_only",
		ReposScope:               "selected",
		RepoIDs:                  []int64{7},
		UseAikidoLibraryForMajor: true,
	})
	if !dep.Enabled.ValueBool() || dep.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("unexpected dependency %#v", dep)
	}
}

func TestMergeDependencyPreservesIgnoredFields(t *testing.T) {
	prior := testPlannedDependency()
	prior.Enabled = types.BoolValue(false)

	dep := mergeDependencyAPIAndPrior(dependencySettingsAPI{
		Enabled:                  false,
		UpgradeType:              "none",
		ReposScope:               "all",
		RepoIDs:                  []int64{},
		UseAikidoLibraryForMajor: false,
	}, &prior)
	if dep.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("dependency.upgrade_type = %s", dep.UpgradeType.ValueString())
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
					"upgrade_type": "none",
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

	res := &dependencySettingsResource{client: client.New(srv.Client(), srv.URL)}
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
	if state.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("upgrade_type = %s", state.UpgradeType.ValueString())
	}
	if state.ID.ValueString() != dependencySettingsResourceID {
		t.Errorf("id = %s, want %s", state.ID.ValueString(), dependencySettingsResourceID)
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

	res := &dependencySettingsResource{client: client.New(srv.Client(), srv.URL)}
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
	res := &dependencySettingsResource{client: client.New(srv.Client(), srv.URL)}

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
	res := &dependencySettingsResource{client: client.New(srv.Client(), srv.URL)}

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

func TestNormalizeIDs_NilIsEmpty(t *testing.T) {
	ids := normalizeIDs(nil)
	if ids == nil {
		t.Fatal("want non-nil empty slice")
	}
	if len(ids) != 0 {
		t.Errorf("ids = %#v, want empty", ids)
	}
}
