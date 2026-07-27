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
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func testPlanned() autofixSettingsModel {
	return autofixSettingsModel{
		Enabled:                  types.BoolValue(true),
		UpgradeType:              types.StringValue("critical_and_high_only"),
		DependencyReposScope:     types.StringValue("selected"),
		DependencyRepoIDs:        []int64{10, 20},
		UseAikidoLibraryForMajor: types.BoolValue(true),
		PentestAutofixType:       types.StringValue("all"),
		SastAutofixType:          types.StringValue("critical_issues_only"),
		SastReposScope:           types.StringValue("selected"),
		SastRepoIDs:              []int64{30, 40},
	}
}

func TestGetAutofixSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != basePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{
			"settings": {
				"enabled": true,
				"upgrade_type": "upgrade_all_packages",
				"dependency_repos_scope": "all",
				"dependency_repo_ids": [1, 2],
				"use_aikido_library_for_major": false,
				"pentest_autofix_type": "none",
				"sast_autofix_type": "all",
				"sast_repos_scope": "selected",
				"sast_repo_ids": [9]
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	got, err := res.getAutofixSettings(context.Background())
	if err != nil {
		t.Fatalf("getAutofixSettings: %v", err)
	}

	want := autofixSettingsAPI{
		Enabled:                  true,
		UpgradeType:              "upgrade_all_packages",
		DependencyReposScope:     "all",
		DependencyRepoIDs:        []int64{1, 2},
		UseAikidoLibraryForMajor: false,
		PentestAutofixType:       "none",
		SastAutofixType:          "all",
		SastReposScope:           "selected",
		SastRepoIDs:              []int64{9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestGetAutofixSettings_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	t.Cleanup(srv.Close)

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	if _, err := res.getAutofixSettings(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestModelFromAPI(t *testing.T) {
	state := modelFromAPI(autofixSettingsAPI{
		Enabled:                  true,
		UpgradeType:              "none",
		DependencyReposScope:     "selected",
		DependencyRepoIDs:        []int64{7},
		UseAikidoLibraryForMajor: true,
		PentestAutofixType:       "critical_and_high_only",
		SastAutofixType:          "none",
		SastReposScope:           "all",
		SastRepoIDs:              []int64{8, 9},
	})

	if !state.Enabled.ValueBool() {
		t.Error("enabled = false, want true")
	}
	if state.UpgradeType.ValueString() != "none" {
		t.Errorf("upgrade_type = %s", state.UpgradeType.ValueString())
	}
	if state.DependencyReposScope.ValueString() != "selected" {
		t.Errorf("dependency_repos_scope = %s", state.DependencyReposScope.ValueString())
	}
	if !reflect.DeepEqual(state.DependencyRepoIDs, []int64{7}) {
		t.Errorf("dependency_repo_ids = %#v", state.DependencyRepoIDs)
	}
	if !state.UseAikidoLibraryForMajor.ValueBool() {
		t.Error("use_aikido_library_for_major = false, want true")
	}
	if state.PentestAutofixType.ValueString() != "critical_and_high_only" {
		t.Errorf("pentest_autofix_type = %s", state.PentestAutofixType.ValueString())
	}
	if state.SastAutofixType.ValueString() != "none" {
		t.Errorf("sast_autofix_type = %s", state.SastAutofixType.ValueString())
	}
	if state.SastReposScope.ValueString() != "all" {
		t.Errorf("sast_repos_scope = %s", state.SastReposScope.ValueString())
	}
	if !reflect.DeepEqual(state.SastRepoIDs, []int64{8, 9}) {
		t.Errorf("sast_repo_ids = %#v", state.SastRepoIDs)
	}
}

func TestModelFromAPI_NilListsBecomeEmpty(t *testing.T) {
	state := modelFromAPI(autofixSettingsAPI{})
	if state.DependencyRepoIDs == nil {
		t.Error("dependency_repo_ids is nil, want empty slice")
	}
	if len(state.DependencyRepoIDs) != 0 {
		t.Errorf("dependency_repo_ids = %#v, want empty", state.DependencyRepoIDs)
	}
	if state.SastRepoIDs == nil {
		t.Error("sast_repo_ids is nil, want empty slice")
	}
	if len(state.SastRepoIDs) != 0 {
		t.Errorf("sast_repo_ids = %#v, want empty", state.SastRepoIDs)
	}
}

func TestRequestBody_Full(t *testing.T) {
	body := requestBody(testPlanned())

	want := map[string]any{
		"enabled":                      true,
		"upgrade_type":                 "critical_and_high_only",
		"dependency_repos_scope":       "selected",
		"dependency_repo_ids":          []int64{10, 20},
		"use_aikido_library_for_major": true,
		"pentest_autofix_type":         "all",
		"sast_autofix_type":            "critical_issues_only",
		"sast_repos_scope":             "selected",
		"sast_repo_ids":                []int64{30, 40},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestRequestBody_OmitsUnknownDependencyFields(t *testing.T) {
	planned := testPlanned()
	planned.UpgradeType = types.StringUnknown()
	planned.DependencyReposScope = types.StringUnknown()

	body := requestBody(planned)

	for _, key := range []string{"upgrade_type", "dependency_repos_scope"} {
		if _, ok := body[key]; ok {
			t.Errorf("body unexpectedly includes %s: %#v", key, body[key])
		}
	}
	if body["enabled"] != true {
		t.Errorf("enabled = %#v, want true", body["enabled"])
	}
	if !reflect.DeepEqual(body["dependency_repo_ids"], []int64{10, 20}) {
		t.Errorf("dependency_repo_ids = %#v", body["dependency_repo_ids"])
	}
	if !reflect.DeepEqual(body["sast_repo_ids"], []int64{30, 40}) {
		t.Errorf("sast_repo_ids = %#v", body["sast_repo_ids"])
	}
}

func TestRequestBody_NilIDsBecomeEmpty(t *testing.T) {
	planned := testPlanned()
	planned.DependencyRepoIDs = nil
	planned.SastRepoIDs = nil

	body := requestBody(planned)

	if !reflect.DeepEqual(body["dependency_repo_ids"], []int64{}) {
		t.Errorf("dependency_repo_ids = %#v, want empty slice", body["dependency_repo_ids"])
	}
	if !reflect.DeepEqual(body["sast_repo_ids"], []int64{}) {
		t.Errorf("sast_repo_ids = %#v, want empty slice", body["sast_repo_ids"])
	}
}

func TestApplySettings_EchoesPlannedWhenAPIRewrites(t *testing.T) {
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != basePath {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPut:
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Errorf("decoding PUT body: %v", err)
			}
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			// API silently forces these when enabled=false.
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": false,
					"upgrade_type": "none",
					"dependency_repos_scope": "all",
					"dependency_repo_ids": [],
					"use_aikido_library_for_major": true,
					"pentest_autofix_type": "all",
					"sast_autofix_type": "critical_issues_only",
					"sast_repos_scope": "selected",
					"sast_repo_ids": [30, 40]
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlanned()
	planned.Enabled = types.BoolValue(false)
	planned.UpgradeType = types.StringValue("critical_and_high_only")
	planned.DependencyReposScope = types.StringValue("selected")
	planned.DependencyRepoIDs = []int64{10, 20}

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}

	if putBody["enabled"] != false {
		t.Errorf("PUT enabled = %#v, want false", putBody["enabled"])
	}
	if putBody["upgrade_type"] != "critical_and_high_only" {
		t.Errorf("PUT upgrade_type = %#v", putBody["upgrade_type"])
	}

	// State must keep planned values for fields the API rewrites, so Terraform
	// does not report an inconsistent result after apply.
	if state.Enabled.ValueBool() {
		t.Error("enabled = true, want false from API")
	}
	if state.UpgradeType.ValueString() != "critical_and_high_only" {
		t.Errorf("upgrade_type = %s, want planned value", state.UpgradeType.ValueString())
	}
	if state.DependencyReposScope.ValueString() != "selected" {
		t.Errorf("dependency_repos_scope = %s, want planned value", state.DependencyReposScope.ValueString())
	}
	if !reflect.DeepEqual(state.DependencyRepoIDs, []int64{10, 20}) {
		t.Errorf("dependency_repo_ids = %#v, want planned [10 20]", state.DependencyRepoIDs)
	}
	// Non-rewritten fields come from the API refresh.
	if state.SastAutofixType.ValueString() != "critical_issues_only" {
		t.Errorf("sast_autofix_type = %s", state.SastAutofixType.ValueString())
	}
}

func TestApplySettings_UsesAPIWhenPlannedUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"settings": {
					"enabled": true,
					"upgrade_type": "minor_and_patch_versions_only",
					"dependency_repos_scope": "all",
					"dependency_repo_ids": [],
					"use_aikido_library_for_major": false,
					"pentest_autofix_type": "none",
					"sast_autofix_type": "all",
					"sast_repos_scope": "all",
					"sast_repo_ids": []
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlanned()
	planned.UpgradeType = types.StringUnknown()
	planned.DependencyReposScope = types.StringUnknown()

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}

	if state.UpgradeType.ValueString() != "minor_and_patch_versions_only" {
		t.Errorf("upgrade_type = %s, want API value", state.UpgradeType.ValueString())
	}
	if state.DependencyReposScope.ValueString() != "all" {
		t.Errorf("dependency_repos_scope = %s, want API value", state.DependencyReposScope.ValueString())
	}
	// Required set attributes are always known; planned IDs are echoed.
	if !reflect.DeepEqual(state.DependencyRepoIDs, []int64{10, 20}) {
		t.Errorf("dependency_repo_ids = %#v, want planned", state.DependencyRepoIDs)
	}
}

func TestApplySettings_PUTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
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

func TestApplySettings_GETErrorAfterPUT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "read failed")
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(srv.Close)

	res := &autofixSettingsResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applySettings(context.Background(), testPlanned())
	if !diags.HasError() {
		t.Fatal("expected diagnostics error on GET failure")
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
