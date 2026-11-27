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

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Shared helpers (ID maps, missing-ID checks, import parsing, GET path) are
// covered in task_tracking_team_mapping_test.go. These tests cover what is
// specific to the code repo resource.

func testProjectReposMap(t *testing.T, m map[string][]int64) types.Map {
	t.Helper()
	tfMap, diags := idMapFromAPI(context.Background(), m)
	if diags.HasError() {
		t.Fatalf("idMapFromAPI: %v", diags)
	}
	return tfMap
}

func testPlannedMapping(t *testing.T) taskTrackingCodeRepoMappingModel {
	t.Helper()
	return taskTrackingCodeRepoMappingModel{
		ProjectReposMap: testProjectReposMap(t, map[string][]int64{
			"10000": {1, 2, 3},
			"10001": {4, 6},
		}),
	}
}

func TestConstructCodeRepoMappingBody(t *testing.T) {
	planned := map[string][]int64{
		"10000": {1, 2, 3},
		"10001": nil,
	}

	body := constructMappingBody(projectReposMapAttribute, planned, types.Int64Null())
	got, ok := body["project_repos_map"].(map[string][]int64)
	if !ok {
		t.Fatalf("project_repos_map type = %T", body["project_repos_map"])
	}
	if !reflect.DeepEqual(got["10000"], []int64{1, 2, 3}) {
		t.Errorf("10000 = %#v", got["10000"])
	}
	if got["10001"] == nil {
		t.Error("nil repo list should be normalized to empty slice")
	}
	if _, ok := body["integration_id"]; ok {
		t.Errorf("did not expect integration_id, body = %#v", body)
	}
}

func TestMappingResourceID_CodeRepo(t *testing.T) {
	if got := mappingResourceID(types.Int64Null(), taskTrackingCodeRepoMappingResourceID); got != taskTrackingCodeRepoMappingResourceID {
		t.Errorf("null id = %s", got)
	}
	if got := mappingResourceID(types.Int64Value(7), taskTrackingCodeRepoMappingResourceID); got != "7" {
		t.Errorf("integration id = %s, want 7", got)
	}
}

func TestParseMappingImportID_CodeRepo(t *testing.T) {
	id, integrationID, err := parseMappingImportID(taskTrackingCodeRepoMappingResourceID, taskTrackingCodeRepoMappingResourceID)
	if err != nil {
		t.Fatalf("sentinel: %v", err)
	}
	if id != taskTrackingCodeRepoMappingResourceID || !integrationID.IsNull() {
		t.Errorf("sentinel id=%s integration=%v", id, integrationID)
	}

	if _, _, err := parseMappingImportID("team", taskTrackingCodeRepoMappingResourceID); err == nil {
		t.Fatal("expected error for a non-sentinel, non-numeric import ID")
	}
}

func TestRepoMapFromAPI_TeamsModeIsEmpty(t *testing.T) {
	got := repoMapFromAPI(projectMappingAPI{
		MappingMode: mappingModeTeams,
		ProjectRepoMap: map[string][]int64{
			"10000": {1},
		},
	})
	if len(got) != 0 {
		t.Errorf("teams mode map = %#v, want empty", got)
	}

	got = repoMapFromAPI(projectMappingAPI{
		MappingMode: mappingModeRepos,
		ProjectRepoMap: map[string][]int64{
			"10000": {1},
		},
	})
	if !reflect.DeepEqual(got, map[string][]int64{"10000": {1}}) {
		t.Errorf("repos mode map = %#v", got)
	}
}

func TestApplyMapping_Success(t *testing.T) {
	var postBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == mapCodeReposToProjectsPath:
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&postBody); err != nil {
				t.Errorf("decoding POST body: %v", err)
			}
			_, _ = io.WriteString(w, `{"success":1}`)
		case r.Method == http.MethodGet && r.URL.Path == projectMappingPath:
			_, _ = io.WriteString(w, `{
				"mapping_mode": "repos",
				"project_repo_map": {
					"10000": [1, 2, 3],
					"10001": [4, 6],
					"10002": []
				}
			}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	planned := testPlannedMapping(t)
	planned.IntegrationID = types.Int64Value(2)

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.applyMapping(context.Background(), planned)
	if diags.HasError() {
		t.Fatalf("applyMapping: %v", diags)
	}
	if state.ID.ValueString() != "2" {
		t.Errorf("id = %s, want 2", state.ID.ValueString())
	}
	if postBody["integration_id"] != float64(2) {
		t.Errorf("POST integration_id = %#v, want 2", postBody["integration_id"])
	}
	gotMap, _ := postBody["project_repos_map"].(map[string]any)
	if gotMap["10000"] == nil {
		t.Errorf("POST body missing 10000: %#v", postBody)
	}
}

func TestApplyMapping_POSTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == mapCodeReposToProjectsPath {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "invalid mapping")
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applyMapping(context.Background(), testPlannedMapping(t))
	if !diags.HasError() {
		t.Fatal("expected diagnostics error on POST failure")
	}
}

func TestApplyMapping_ErrorsOnDroppedRepoIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"mapping_mode": "repos",
				"project_repo_map": {
					"10000": [1, 2]
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applyMapping(context.Background(), testPlannedMapping(t))
	if !diags.HasError() {
		t.Fatal("expected error when the API drops repo IDs")
	}

	var mentioned bool
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), "3") && strings.Contains(d.Detail(), "10000") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("error should name project 10000 and dropped ID 3, got: %v", diags)
	}
}

func TestApplyMapping_ErrorsOnTeamsMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"mapping_mode": "teams",
				"project_repo_map": null,
				"project_teams_map": {"10000": [1, 2, 3]}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applyMapping(context.Background(), testPlannedMapping(t))
	if !diags.HasError() {
		t.Fatal("expected error when mapping mode stays teams")
	}
}

func TestReadMapping_DropsEmptyProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"mapping_mode": "repos",
			"project_repo_map": {
				"10000": [1, 2, 3],
				"10001": [4, 6],
				"10002": []
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.readMapping(context.Background(), testPlannedMapping(t))
	if diags.HasError() {
		t.Fatalf("readMapping: %v", diags)
	}

	got, mapDiags := idMapToAPI(context.Background(), state.ProjectReposMap, projectReposMapAttribute)
	if mapDiags.HasError() {
		t.Fatalf("idMapToAPI: %v", mapDiags)
	}
	if _, ok := got["10002"]; ok {
		t.Errorf("empty project 10002 should be omitted, got %#v", got)
	}
	if !reflect.DeepEqual(got["10000"], []int64{1, 2, 3}) {
		t.Errorf("10000 = %#v", got["10000"])
	}
}

func TestReadMapping_KeepsConfiguredEmptyProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"mapping_mode": "repos",
			"project_repo_map": {
				"10000": [1],
				"10002": []
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	prior := taskTrackingCodeRepoMappingModel{
		ProjectReposMap: testProjectReposMap(t, map[string][]int64{
			"10000": {1},
			"10002": {},
		}),
	}

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.readMapping(context.Background(), prior)
	if diags.HasError() {
		t.Fatalf("readMapping: %v", diags)
	}

	got, _ := idMapToAPI(context.Background(), state.ProjectReposMap, projectReposMapAttribute)
	if _, ok := got["10002"]; !ok {
		t.Errorf("configured empty project 10002 should be kept, got %#v", got)
	}
}

func TestReadMapping_NotFoundRemovesResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.readMapping(context.Background(), testPlannedMapping(t))
	if diags.HasError() {
		t.Fatalf("readMapping: %v", diags)
	}
	if state != nil {
		t.Errorf("state = %#v, want nil", state)
	}
}

func TestDelete_LeavesRemoteUnchanged(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	res := &taskTrackingCodeRepoMappingResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	planned := testPlannedMapping(t)
	diags := state.Set(ctx, planned)
	if diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}

	var resp resource.DeleteResponse
	res.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if called {
		t.Error("Delete should not call the API")
	}
}
