package resources

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/helpers"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	projectMappingPath = "/public/v1/task_tracking/projectMapping"
	mappingModeTeams   = "teams"
	mappingModeRepos   = "repos"
)

type projectMappingAPI struct {
	MappingMode     string             `json:"mapping_mode"`
	ProjectRepoMap  map[string][]int64 `json:"project_repo_map"`
	ProjectTeamsMap map[string][]int64 `json:"project_teams_map"`
}

func getProjectMapping(ctx context.Context, apiClient *client.Client, integrationID types.Int64) (projectMappingAPI, error) {
	var response projectMappingAPI
	if err := apiClient.Do(ctx, "GET", projectMappingGETPath(integrationID), nil, &response); err != nil {
		return projectMappingAPI{}, err
	}
	return response, nil
}

func projectMappingGETPath(integrationID types.Int64) string {
	if integrationID.IsNull() || integrationID.IsUnknown() {
		return projectMappingPath
	}
	return projectMappingPath + "?integration_id=" + strconv.FormatInt(integrationID.ValueInt64(), 10)
}

func mappingResourceID(integrationID types.Int64) string {
	if integrationID.IsNull() || integrationID.IsUnknown() {
		return taskTrackingTeamMappingResourceID
	}
	return strconv.FormatInt(integrationID.ValueInt64(), 10)
}

func parseMappingImportID(id string) (string, types.Int64, error) {
	if id == taskTrackingTeamMappingResourceID {
		return id, types.Int64Null(), nil
	}

	integrationID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || integrationID < 1 {
		return "", types.Int64Null(), fmt.Errorf("use %q when no integration_id is set, or the numeric integration ID", taskTrackingTeamMappingResourceID)
	}

	return id, types.Int64Value(integrationID), nil
}

func idMapToAPI(ctx context.Context, m types.Map, attribute string) (map[string][]int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.IsNull() || m.IsUnknown() {
		return map[string][]int64{}, diags
	}

	result := make(map[string][]int64, len(m.Elements()))
	for key, value := range m.Elements() {
		set, ok := value.(types.Set)
		if !ok {
			diags.AddError(
				"Invalid "+attribute,
				fmt.Sprintf("expected a set of IDs for project %q, got %T", key, value),
			)
			continue
		}

		var ids []int64
		diags.Append(set.ElementsAs(ctx, &ids, false)...)
		result[key] = helpers.NormalizeIDs(ids)
	}

	return result, diags
}

func idMapFromAPI(ctx context.Context, api map[string][]int64) (types.Map, diag.Diagnostics) {
	setType := types.SetType{ElemType: types.Int64Type}
	if api == nil {
		api = map[string][]int64{}
	}

	elems := make(map[string]attr.Value, len(api))
	var diags diag.Diagnostics
	for key, ids := range api {
		set, setDiags := types.SetValueFrom(ctx, types.Int64Type, helpers.NormalizeIDs(ids))
		diags.Append(setDiags...)
		if setDiags.HasError() {
			return types.MapNull(setType), diags
		}
		elems[key] = set
	}

	m, mapDiags := types.MapValue(setType, elems)
	diags.Append(mapDiags...)
	return m, diags
}

func emptyMapKeys(ctx context.Context, m types.Map, attribute string) (map[string]struct{}, diag.Diagnostics) {
	keys := make(map[string]struct{})
	api, diags := idMapToAPI(ctx, m, attribute)
	if diags.HasError() {
		return keys, diags
	}
	for key, ids := range api {
		if len(ids) == 0 {
			keys[key] = struct{}{}
		}
	}
	return keys, diags
}

// normalizeIDMap drops empty projects that are not already in config, so
// unmapped tracker projects do not show up as drift.
func normalizeIDMap(api map[string][]int64, keepEmpty map[string]struct{}) map[string][]int64 {
	out := make(map[string][]int64)
	for key, ids := range api {
		normalized := helpers.NormalizeIDs(ids)
		if len(normalized) == 0 {
			if _, ok := keepEmpty[key]; !ok {
				continue
			}
		}
		out[key] = normalized
	}
	return out
}

func missingIDMappings(planned, actual map[string][]int64) map[string][]int64 {
	missing := make(map[string][]int64)
	for key, plannedIDs := range planned {
		dropped := helpers.DroppedRepoIDs(plannedIDs, actual[key])
		if len(dropped) > 0 {
			missing[key] = dropped
		}
	}
	return missing
}

func formatMissingMappings(missing map[string][]int64, valueLabel string) string {
	projectIDs := make([]string, 0, len(missing))
	for projectID := range missing {
		projectIDs = append(projectIDs, projectID)
	}
	slices.Sort(projectIDs)

	parts := make([]string, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		parts = append(parts, fmt.Sprintf("project %s (missing %s IDs: %v)", projectID, valueLabel, missing[projectID]))
	}
	return strings.Join(parts, "; ")
}

func teamsMapFromAPI(api projectMappingAPI) map[string][]int64 {
	if api.MappingMode != "" && api.MappingMode != mappingModeTeams {
		return map[string][]int64{}
	}
	return api.ProjectTeamsMap
}

func constructMappingBody(field string, ids map[string][]int64, integrationID types.Int64) map[string]any {
	normalized := make(map[string][]int64, len(ids))
	for key, values := range ids {
		normalized[key] = helpers.NormalizeIDs(values)
	}

	body := map[string]any{
		field: normalized,
	}
	if !integrationID.IsNull() && !integrationID.IsUnknown() {
		body["integration_id"] = integrationID.ValueInt64()
	}
	return body
}
