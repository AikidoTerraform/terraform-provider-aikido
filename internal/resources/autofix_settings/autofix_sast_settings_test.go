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

func testPlannedSast() sastModel {
	return sastModel{
		Enabled:     types.BoolValue(true),
		AutofixType: types.StringValue("critical_issues_only"),
		ReposScope:  types.StringValue("selected"),
		RepoIDs:     []int64{30, 40},
	}
}

func TestConstructSastBody_Enabled(t *testing.T) {
	planned := testPlannedSast()
	body := constructSastBody(&planned)
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
				"autofix_type": "all",
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
	if !sast.Enabled || sast.AutofixType != "all" || !reflect.DeepEqual(sast.RepoIDs, []int64{9}) {
		t.Errorf("unexpected sast %#v", sast)
	}
}

func TestMapSastAPIToModel(t *testing.T) {
	sast := mapSastAPIToModel(sastSettingsAPI{
		Enabled:     false,
		AutofixType: "none",
		ReposScope:  "none",
		RepoIDs:     []int64{},
	})
	if sast.Enabled.ValueBool() || sast.AutofixType.ValueString() != "none" {
		t.Errorf("unexpected sast %#v", sast)
	}
}

func TestMergeSastPreservesIgnoredFields(t *testing.T) {
	prior := testPlannedSast()
	prior.Enabled = types.BoolValue(false)

	sast := mergeSastAPIAndPrior(sastSettingsAPI{
		Enabled:     false,
		AutofixType: "none",
		ReposScope:  "none",
		RepoIDs:     []int64{},
	}, &prior)
	if sast.AutofixType.ValueString() != "critical_issues_only" {
		t.Errorf("sast.autofix_type = %s", sast.AutofixType.ValueString())
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
					"autofix_type": "none",
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

	res := &sastSettingsResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applySettings(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applySettings: %v", diags)
	}

	if state.ReposScope.ValueString() != "selected" {
		t.Errorf("repos_scope = %s", state.ReposScope.ValueString())
	}
	if state.ID.ValueString() != sastSettingsResourceID {
		t.Errorf("id = %s, want %s", state.ID.ValueString(), sastSettingsResourceID)
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
	res := &sastSettingsResource{client: client.New(srv.Client(), srv.URL)}

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
