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

func testProjectTeamsMap(t *testing.T, m map[string][]int64) types.Map {
	t.Helper()
	tfMap, diags := idMapFromAPI(context.Background(), m)
	if diags.HasError() {
		t.Fatalf("idMapFromAPI: %v", diags)
	}
	return tfMap
}

func testPlannedTeamMapping(t *testing.T) taskTrackingTeamMappingModel {
	t.Helper()
	return taskTrackingTeamMappingModel{
		ProjectTeamsMap: testProjectTeamsMap(t, map[string][]int64{
			"10000": {1, 2, 3},
			"10001": {4, 6},
		}),
	}
}

func TestConstructTeamMappingBody(t *testing.T) {
	planned := map[string][]int64{
		"10000": {1, 2, 3},
		"10001": nil,
	}

	body := constructMappingBody(projectTeamsMapAttribute, planned, types.Int64Null())
	got, ok := body["project_teams_map"].(map[string][]int64)
	if !ok {
		t.Fatalf("project_teams_map type = %T", body["project_teams_map"])
	}
	if !reflect.DeepEqual(got["10000"], []int64{1, 2, 3}) {
		t.Errorf("10000 = %#v", got["10000"])
	}
	if got["10001"] == nil {
		t.Error("nil team list should be normalized to empty slice")
	}
	if _, ok := body["integration_id"]; ok {
		t.Errorf("did not expect integration_id, body = %#v", body)
	}

	body = constructMappingBody(projectTeamsMapAttribute, planned, types.Int64Value(2))
	if body["integration_id"] != int64(2) {
		t.Errorf("integration_id = %#v, want 2", body["integration_id"])
	}
}

func TestProjectMappingGETPath(t *testing.T) {
	if got := projectMappingGETPath(types.Int64Null()); got != projectMappingPath {
		t.Errorf("null integration path = %s", got)
	}
	if got := projectMappingGETPath(types.Int64Unknown()); got != projectMappingPath {
		t.Errorf("unknown integration path = %s", got)
	}
	if got := projectMappingGETPath(types.Int64Value(2)); got != projectMappingPath+"?integration_id=2" {
		t.Errorf("set integration path = %s", got)
	}
}

func TestTeamMappingResourceID(t *testing.T) {
	if got := mappingResourceID(types.Int64Null()); got != taskTrackingTeamMappingResourceID {
		t.Errorf("null id = %s", got)
	}
	if got := mappingResourceID(types.Int64Value(7)); got != "7" {
		t.Errorf("integration id = %s, want 7", got)
	}
}

func TestNormalizeIDMap(t *testing.T) {
	api := map[string][]int64{
		"10000": {1, 2},
		"10001": {},
		"10002": nil,
		"10003": {4},
	}
	keepEmpty := map[string]struct{}{"10001": {}}

	got := normalizeIDMap(api, keepEmpty)
	want := map[string][]int64{
		"10000": {1, 2},
		"10001": {},
		"10003": {4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalize = %#v, want %#v", got, want)
	}

	if got := normalizeIDMap(nil, nil); len(got) != 0 {
		t.Errorf("nil api = %#v, want empty", got)
	}
}

func TestTeamsMapFromAPI_ReposModeIsEmpty(t *testing.T) {
	got := teamsMapFromAPI(projectMappingAPI{
		MappingMode: mappingModeRepos,
		ProjectTeamsMap: map[string][]int64{
			"10000": {1},
		},
	})
	if len(got) != 0 {
		t.Errorf("repos mode team map = %#v, want empty", got)
	}

	got = teamsMapFromAPI(projectMappingAPI{
		MappingMode: mappingModeTeams,
		ProjectTeamsMap: map[string][]int64{
			"10000": {1},
		},
	})
	if !reflect.DeepEqual(got, map[string][]int64{"10000": {1}}) {
		t.Errorf("teams mode map = %#v", got)
	}
}

func TestMissingIDMappings(t *testing.T) {
	planned := map[string][]int64{
		"10000": {1, 2, 3},
		"10001": {4},
		"10002": {},
	}
	actual := map[string][]int64{
		"10000": {1, 2},
		"10002": {},
	}

	got := missingIDMappings(planned, actual)
	want := map[string][]int64{
		"10000": {3},
		"10001": {4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("missing = %#v, want %#v", got, want)
	}
}

func TestFormatMissingMappings_SortsProjects(t *testing.T) {
	got := formatMissingMappings(map[string][]int64{
		"10001": {4},
		"10000": {3},
	}, "team")
	want := "project 10000 (missing team IDs: [3]); project 10001 (missing team IDs: [4])"
	if got != want {
		t.Errorf("format = %q, want %q", got, want)
	}
}

func TestIDMapRoundTrip(t *testing.T) {
	ctx := context.Background()
	want := map[string][]int64{
		"10000": {1, 2, 3},
		"10001": {},
	}

	tfMap := testProjectTeamsMap(t, want)
	got, diags := idMapToAPI(ctx, tfMap, projectTeamsMapAttribute)
	if diags.HasError() {
		t.Fatalf("idMapToAPI: %v", diags)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %#v, want %#v", got, want)
	}
}

func TestGetProjectMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != projectMappingPath {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("integration_id"); got != "2" {
			t.Errorf("integration_id = %q, want 2", got)
		}
		_, _ = io.WriteString(w, `{
			"mapping_mode": "teams",
			"project_teams_map": {
				"10000": [1, 2, 3],
				"10001": [4, 6],
				"10002": []
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	api, err := getProjectMapping(context.Background(), client.New(srv.Client(), srv.URL), types.Int64Value(2))
	if err != nil {
		t.Fatalf("getProjectMapping: %v", err)
	}
	if api.MappingMode != mappingModeTeams {
		t.Errorf("mapping_mode = %s", api.MappingMode)
	}
	if !reflect.DeepEqual(api.ProjectTeamsMap["10000"], []int64{1, 2, 3}) {
		t.Errorf("10000 = %#v", api.ProjectTeamsMap["10000"])
	}
}

func TestApplyTeamMapping_Success(t *testing.T) {
	var postBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == mapTeamsToProjectsPath:
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&postBody); err != nil {
				t.Errorf("decoding POST body: %v", err)
			}
			_, _ = io.WriteString(w, `{"success":1}`)
		case r.Method == http.MethodGet && r.URL.Path == projectMappingPath:
			_, _ = io.WriteString(w, `{
				"mapping_mode": "teams",
				"project_teams_map": {
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

	planned := testPlannedTeamMapping(t)
	planned.IntegrationID = types.Int64Value(2)

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
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
	gotMap, _ := postBody["project_teams_map"].(map[string]any)
	if gotMap["10000"] == nil {
		t.Errorf("POST body missing 10000: %#v", postBody)
	}
}

func TestApplyTeamMapping_POSTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == mapTeamsToProjectsPath {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "invalid mapping")
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applyMapping(context.Background(), testPlannedTeamMapping(t))
	if !diags.HasError() {
		t.Fatal("expected diagnostics error on POST failure")
	}
}

func TestApplyTeamMapping_ErrorsOnDroppedTeamIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"mapping_mode": "teams",
				"project_teams_map": {
					"10000": [1, 2]
				}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applyMapping(context.Background(), testPlannedTeamMapping(t))
	if !diags.HasError() {
		t.Fatal("expected error when the API drops team IDs")
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

func TestApplyTeamMapping_ErrorsOnReposMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"success":1}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{
				"mapping_mode": "repos",
				"project_teams_map": null,
				"project_repo_map": {"10000": [1, 2, 3]}
			}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
	_, diags := res.applyMapping(context.Background(), testPlannedTeamMapping(t))
	if !diags.HasError() {
		t.Fatal("expected error when mapping mode stays repos")
	}
}

func TestReadTeamMapping_DropsEmptyProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"mapping_mode": "teams",
			"project_teams_map": {
				"10000": [1, 2, 3],
				"10001": [4, 6],
				"10002": []
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.readMapping(context.Background(), testPlannedTeamMapping(t))
	if diags.HasError() {
		t.Fatalf("readMapping: %v", diags)
	}

	got, mapDiags := idMapToAPI(context.Background(), state.ProjectTeamsMap, projectTeamsMapAttribute)
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

func TestReadTeamMapping_KeepsConfiguredEmptyProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"mapping_mode": "teams",
			"project_teams_map": {
				"10000": [1],
				"10002": []
			}
		}`)
	}))
	t.Cleanup(srv.Close)

	prior := taskTrackingTeamMappingModel{
		ProjectTeamsMap: testProjectTeamsMap(t, map[string][]int64{
			"10000": {1},
			"10002": {},
		}),
	}

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.readMapping(context.Background(), prior)
	if diags.HasError() {
		t.Fatalf("readMapping: %v", diags)
	}

	got, _ := idMapToAPI(context.Background(), state.ProjectTeamsMap, projectTeamsMapAttribute)
	if _, ok := got["10002"]; !ok {
		t.Errorf("configured empty project 10002 should be kept, got %#v", got)
	}
}

func TestReadTeamMapping_NotFoundRemovesResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	t.Cleanup(srv.Close)

	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}
	state, diags := res.readMapping(context.Background(), testPlannedTeamMapping(t))
	if diags.HasError() {
		t.Fatalf("readMapping: %v", diags)
	}
	if state != nil {
		t.Errorf("state = %#v, want nil", state)
	}
}

func TestTeamMappingDelete_LeavesRemoteUnchanged(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	res := &taskTrackingTeamMappingResource{client: client.New(srv.Client(), srv.URL)}

	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{Schema: schemaResp.Schema}
	planned := testPlannedTeamMapping(t)
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

func TestParseTeamMappingImportID(t *testing.T) {
	id, integrationID, err := parseMappingImportID(taskTrackingTeamMappingResourceID)
	if err != nil {
		t.Fatalf("sentinel: %v", err)
	}
	if id != taskTrackingTeamMappingResourceID || !integrationID.IsNull() {
		t.Errorf("sentinel id=%s integration=%v", id, integrationID)
	}

	id, integrationID, err = parseMappingImportID("2")
	if err != nil {
		t.Fatalf("numeric: %v", err)
	}
	if id != "2" || integrationID.ValueInt64() != 2 {
		t.Errorf("numeric id=%s integration=%d", id, integrationID.ValueInt64())
	}

	if _, _, err := parseMappingImportID("linear"); err == nil {
		t.Fatal("expected error for non-numeric import ID")
	}
	if _, _, err := parseMappingImportID("0"); err == nil {
		t.Fatal("expected error for integration ID 0")
	}
}
