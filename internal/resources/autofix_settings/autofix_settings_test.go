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

func testPlanned() autofixSettingsModel {
	return autofixSettingsModel{
		Dependency: &dependencyModel{
			Enabled:                  types.BoolValue(true),
			UpgradeType:              types.StringValue("critical_and_high_only"),
			ReposScope:               types.StringValue("selected"),
			RepoIDs:                  []int64{10, 20},
			UseAikidoLibraryForMajor: types.BoolValue(true),
		},
		Sast: &sastModel{
			Enabled:     types.BoolValue(true),
			AutofixType: types.StringValue("critical_issues_only"),
			ReposScope:  types.StringValue("selected"),
			RepoIDs:     []int64{30, 40},
		},
		Pentest: &pentestModel{
			Enabled:     types.BoolValue(true),
			AutofixType: types.StringValue("all"),
		},
	}
}

func TestConstructScaBody_Enabled(t *testing.T) {
	body := constructScaBody(testPlanned().Dependency)
	want := map[string]any{
		"enabled":                      true,
		"upgrade_type":                 "critical_and_high_only",
		"dependency_repos_scope":       "selected",
		"dependency_repo_ids":          []int64{10, 20},
		"use_aikido_library_for_major": true,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructScaBody_Disabled(t *testing.T) {
	planned := testPlanned().Dependency
	planned.Enabled = types.BoolValue(false)
	body := constructScaBody(planned)
	want := map[string]any{"enabled": false}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructSastBody_Enabled(t *testing.T) {
	body := constructSastBody(testPlanned().Sast)
	want := map[string]any{
		"enabled":      true,
		"autofix_type": "critical_issues_only",
		"repos_scope":  "selected",
		"repo_ids":     []int64{30, 40},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructSastBody_Disabled(t *testing.T) {
	planned := testPlanned().Sast
	planned.Enabled = types.BoolValue(false)
	body := constructSastBody(planned)
	want := map[string]any{"enabled": false}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructPentestBody_Enabled(t *testing.T) {
	body := constructPentestBody(testPlanned().Pentest)
	want := map[string]any{
		"enabled":      true,
		"autofix_type": "all",
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestConstructPentestBody_Disabled(t *testing.T) {
	planned := testPlanned().Pentest
	planned.Enabled = types.BoolValue(false)
	body := constructPentestBody(planned)
	want := map[string]any{"enabled": false}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestGetFeatureSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case scaSettingsPath:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"upgrade_type": "upgrade_all_packages",
					"repos_scope": "all",
					"repo_ids": [],
					"use_aikido_library_for_major": false
				}
			}`)
		case sastSettingsPath:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"autofix_type": "all",
					"repos_scope": "selected",
					"repo_ids": [9]
				}
			}`)
		case pentestSettingsPath:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": false,
					"autofix_type": "none"
				}
			}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	apiClient := client.New(srv.Client(), srv.URL)

	sca, err := getScaSettings(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("getScaSettings: %v", err)
	}
	if !sca.Enabled || sca.UpgradeType != "upgrade_all_packages" || sca.ReposScope != "all" {
		t.Errorf("unexpected sca %#v", sca)
	}

	sast, err := getSastSettings(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("getSastSettings: %v", err)
	}
	if !sast.Enabled || sast.AutofixType != "all" || !reflect.DeepEqual(sast.RepoIDs, []int64{9}) {
		t.Errorf("unexpected sast %#v", sast)
	}

	pentest, err := getPentestSettings(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("getPentestSettings: %v", err)
	}
	if pentest.Enabled || pentest.AutofixType != "none" {
		t.Errorf("unexpected pentest %#v", pentest)
	}
}

func TestMapAPIToModel(t *testing.T) {
	dependency := mapScaAPIToModel(scaSettingsAPI{
		Enabled:                  true,
		UpgradeType:              "critical_and_high_only",
		ReposScope:               "selected",
		RepoIDs:                  []int64{7},
		UseAikidoLibraryForMajor: true,
	})
	if !dependency.Enabled.ValueBool() || dependency.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("unexpected dependency %#v", dependency)
	}

	sast := mapSastAPIToModel(sastSettingsAPI{
		Enabled:     false,
		AutofixType: "none",
		ReposScope:  "none",
		RepoIDs:     []int64{},
	})
	if sast.Enabled.ValueBool() || sast.AutofixType.ValueString() != "none" {
		t.Errorf("unexpected sast %#v", sast)
	}

	pentest := mapPentestAPIToModel(pentestSettingsAPI{
		Enabled:     true,
		AutofixType: "critical_and_high_only",
	})
	if !pentest.Enabled.ValueBool() || pentest.AutofixType.ValueString() != "critical_and_high_only" {
		t.Errorf("unexpected pentest %#v", pentest)
	}
}

func TestMergePreservesIgnoredFields(t *testing.T) {
	prior := testPlanned()
	prior.Dependency.Enabled = types.BoolValue(false)
	prior.Sast.Enabled = types.BoolValue(false)
	prior.Pentest.Enabled = types.BoolValue(false)

	dependency := mergeScaAPIAndPrior(scaSettingsAPI{
		Enabled:                  false,
		UpgradeType:              "none",
		ReposScope:               "all",
		RepoIDs:                  []int64{},
		UseAikidoLibraryForMajor: false,
	}, prior.Dependency)
	if dependency.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("dependency.upgrade_type = %s", dependency.UpgradeType.ValueString())
	}
	if !reflect.DeepEqual(dependency.RepoIDs, []int64{10, 20}) {
		t.Errorf("dependency.repo_ids = %#v", dependency.RepoIDs)
	}

	sast := mergeSastAPIAndPrior(sastSettingsAPI{
		Enabled:     false,
		AutofixType: "none",
		ReposScope:  "none",
		RepoIDs:     []int64{},
	}, prior.Sast)
	if sast.AutofixType.ValueString() != "critical_issues_only" {
		t.Errorf("sast.autofix_type = %s", sast.AutofixType.ValueString())
	}
	if !reflect.DeepEqual(sast.RepoIDs, []int64{30, 40}) {
		t.Errorf("sast.repo_ids = %#v", sast.RepoIDs)
	}

	pentest := mergePentestAPIAndPrior(pentestSettingsAPI{
		Enabled:     false,
		AutofixType: "none",
	}, prior.Pentest)
	if pentest.AutofixType.ValueString() != "all" {
		t.Errorf("pentest.autofix_type = %s", pentest.AutofixType.ValueString())
	}
}

func TestApplySettings_EchoesPlannedWhenAPIRewrites(t *testing.T) {
	putPaths := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			putPaths[r.URL.Path]++
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			switch r.URL.Path {
			case scaSettingsPath:
				_, _ = io.WriteString(w, `{
					"settings": {
						"enabled": false,
						"upgrade_type": "none",
						"repos_scope": "all",
						"repo_ids": [],
						"use_aikido_library_for_major": false
					}
				}`)
			case sastSettingsPath:
				_, _ = io.WriteString(w, `{
					"settings": {
						"enabled": false,
						"autofix_type": "none",
						"repos_scope": "none",
						"repo_ids": []
					}
				}`)
			case pentestSettingsPath:
				_, _ = io.WriteString(w, `{
					"settings": {
						"enabled": false,
						"autofix_type": "none"
					}
				}`)
			default:
				t.Errorf("unexpected GET path %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlanned()
	planned.Dependency.Enabled = types.BoolValue(false)
	planned.Sast.Enabled = types.BoolValue(false)
	planned.Pentest.Enabled = types.BoolValue(false)

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}

	for _, path := range []string{scaSettingsPath, sastSettingsPath, pentestSettingsPath} {
		if putPaths[path] != 1 {
			t.Errorf("PUT %s count = %d, want 1", path, putPaths[path])
		}
	}

	if state.Dependency.Enabled.ValueBool() {
		t.Error("dependency.enabled = true, want false")
	}
	if state.Dependency.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("dependency.upgrade_type = %s", state.Dependency.UpgradeType.ValueString())
	}
	if state.Sast.ReposScope.ValueString() != "selected" {
		t.Errorf("sast.repos_scope = %s", state.Sast.ReposScope.ValueString())
	}
	if state.Pentest.AutofixType.ValueString() != "all" {
		t.Errorf("pentest.autofix_type = %s", state.Pentest.AutofixType.ValueString())
	}
}

func TestApplySettings_PUTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == scaSettingsPath {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "invalid settings")
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applySettings(context.Background(), testPlanned())
	if !diags.HasError() {
		t.Fatal("expected diagnostics error on PUT failure")
	}
}

func TestDelete_DisablesEachFeature(t *testing.T) {
	putBodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding PUT body: %v", err)
		}
		putBodies[r.URL.Path] = body
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	diags := state.Set(ctx, testPlanned())
	if diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}

	var resp resource.DeleteResponse
	res.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}

	for _, path := range []string{scaSettingsPath, sastSettingsPath, pentestSettingsPath} {
		body, ok := putBodies[path]
		if !ok {
			t.Errorf("missing PUT for %s", path)
			continue
		}
		if body["enabled"] != false {
			t.Errorf("%s enabled = %#v, want false", path, body["enabled"])
		}
		if len(body) != 1 {
			t.Errorf("%s body = %#v, want only enabled=false", path, body)
		}
	}
}

func TestDelete_IgnoresNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	diags := state.Set(ctx, testPlanned())
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
