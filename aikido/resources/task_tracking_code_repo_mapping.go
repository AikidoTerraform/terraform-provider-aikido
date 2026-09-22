package resources

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/helpers"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	mapCodeReposToProjectsPath            = "/public/v1/task_tracking/mapCodeReposToProjects"
	projectMappingPath                    = "/public/v1/task_tracking/projectMapping"
	taskTrackingCodeRepoMappingResourceID = "task_tracking_code_repo_mapping"
	projectRepoMapMode                    = "repos"
)

var (
	_ resource.Resource                = &taskTrackingCodeRepoMappingResource{}
	_ resource.ResourceWithImportState = &taskTrackingCodeRepoMappingResource{}
	_ resource.ResourceWithConfigure   = &taskTrackingCodeRepoMappingResource{}
)

func NewTaskTrackingCodeRepoMappingResource() resource.Resource {
	return &taskTrackingCodeRepoMappingResource{}
}

type taskTrackingCodeRepoMappingResource struct {
	client *client.Client
}

type taskTrackingCodeRepoMappingModel struct {
	ID              types.String `tfsdk:"id"`
	IntegrationID   types.Int64  `tfsdk:"integration_id"`
	ProjectReposMap types.Map    `tfsdk:"project_repos_map"`
}

type projectMappingAPI struct {
	MappingMode    string             `json:"mapping_mode"`
	ProjectRepoMap map[string][]int64 `json:"project_repo_map"`
}

func (r *taskTrackingCodeRepoMappingResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_task_tracking_code_repo_mapping"
}

func (r *taskTrackingCodeRepoMappingResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Maps Aikido code repositories to task-tracker projects (Linear, Jira, and others). " +
			"This is repository mapping, not Aikido team mapping; the public API can write repo-to-project links only. " +
			"Define one resource per task-tracker integration. Omit integration_id when the workspace has a single integration. " +
			"The Aikido API has no delete endpoint for this mapping, so destroying this resource " +
			"only removes it from Terraform state and leaves the remote mapping unchanged.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Task tracking code-repo-to-project mapping identifier. The integration ID when set, otherwise a workspace-wide sentinel.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"integration_id": schema.Int64Attribute{
				Optional:    true,
				Description: "Task-tracker integration ID. Required when the workspace has more than one integration.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"project_repos_map": schema.MapAttribute{
				Required:    true,
				ElementType: types.SetType{ElemType: types.Int64Type},
				Description: "Map of task-tracker project IDs to Aikido code repository IDs. " +
					"Project IDs are those of the connected tracker (for example Linear team IDs or Jira project IDs). " +
					"An empty repository set unmaps that project. " +
					"Projects omitted from the map are left unmapped in config; extra mapped projects in Aikido show as drift.",
			},
		},
	}
}

func (r *taskTrackingCodeRepoMappingResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
	if request.ProviderData == nil {
		return
	}

	apiClient, isClient := request.ProviderData.(*client.Client)
	if !isClient {
		response.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", request.ProviderData),
		)
		return
	}

	r.client = apiClient
}

func (r *taskTrackingCodeRepoMappingResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned taskTrackingCodeRepoMappingModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.applyMapping(ctx, planned)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *taskTrackingCodeRepoMappingResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState taskTrackingCodeRepoMappingModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.readMapping(ctx, priorState)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	if state == nil {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *taskTrackingCodeRepoMappingResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned taskTrackingCodeRepoMappingModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.applyMapping(ctx, planned)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

// Delete only removes the resource from Terraform state. The Aikido API has no
// unmap endpoint, so the remote project mapping is left unchanged.
func (r *taskTrackingCodeRepoMappingResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *taskTrackingCodeRepoMappingResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resourceID, integrationID, err := parseMappingImportID(request.ID)
	if err != nil {
		response.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), resourceID)...)
	if !integrationID.IsNull() {
		response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("integration_id"), integrationID)...)
	}
}

func parseMappingImportID(id string) (string, types.Int64, error) {
	if id == taskTrackingCodeRepoMappingResourceID {
		return id, types.Int64Null(), nil
	}

	integrationID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || integrationID < 1 {
		return "", types.Int64Null(), fmt.Errorf("use %q when no integration_id is set, or the numeric integration ID", taskTrackingCodeRepoMappingResourceID)
	}

	return id, types.Int64Value(integrationID), nil
}

func (r *taskTrackingCodeRepoMappingResource) applyMapping(ctx context.Context, planned taskTrackingCodeRepoMappingModel) (taskTrackingCodeRepoMappingModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	plannedMap, mapDiags := projectReposMapToAPI(ctx, planned.ProjectReposMap)
	diags.Append(mapDiags...)
	if diags.HasError() {
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	if err := r.client.Do(ctx, "POST", mapCodeReposToProjectsPath, constructProjectMappingBody(plannedMap, planned.IntegrationID), nil); err != nil {
		diags.AddError("Error configuring task tracking code repo mapping", err.Error())
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	actual, err := getProjectMapping(ctx, r.client, planned.IntegrationID)
	if err != nil {
		diags.AddError("Error reading task tracking code repo mapping", err.Error())
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	if missing := missingRepoMappings(plannedMap, repoMapFromAPI(actual)); len(missing) > 0 {
		diags.AddError(
			"Error configuring task tracking code repo mapping",
			fmt.Sprintf(
				"Aikido did not persist the repository mapping for: %s. "+
					"Check that the project IDs belong to the task tracker integration and that the repository IDs exist. "+
					"If the workspace uses team mapping, applying this resource switches it to repository mapping.",
				formatMissingMappings(missing),
			),
		)
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	state := planned
	state.ID = types.StringValue(mappingResourceID(planned.IntegrationID))
	return state, diags
}

func (r *taskTrackingCodeRepoMappingResource) readMapping(ctx context.Context, prior taskTrackingCodeRepoMappingModel) (*taskTrackingCodeRepoMappingModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	keepEmpty, keyDiags := emptyProjectKeys(ctx, prior.ProjectReposMap)
	diags.Append(keyDiags...)
	if diags.HasError() {
		return nil, diags
	}

	api, err := getProjectMapping(ctx, r.client, prior.IntegrationID)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}
		diags.AddError("Error reading task tracking code repo mapping", err.Error())
		return nil, diags
	}

	tfMap, mapDiags := projectReposMapFromAPI(ctx, normalizeProjectRepoMap(repoMapFromAPI(api), keepEmpty))
	diags.Append(mapDiags...)
	if diags.HasError() {
		return nil, diags
	}

	state := prior
	state.ID = types.StringValue(mappingResourceID(prior.IntegrationID))
	state.ProjectReposMap = tfMap
	return &state, diags
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
		return taskTrackingCodeRepoMappingResourceID
	}
	return strconv.FormatInt(integrationID.ValueInt64(), 10)
}

func constructProjectMappingBody(projectRepos map[string][]int64, integrationID types.Int64) map[string]any {
	normalized := make(map[string][]int64, len(projectRepos))
	for projectID, repoIDs := range projectRepos {
		normalized[projectID] = helpers.NormalizeIDs(repoIDs)
	}

	body := map[string]any{
		"project_repos_map": normalized,
	}
	if !integrationID.IsNull() && !integrationID.IsUnknown() {
		body["integration_id"] = integrationID.ValueInt64()
	}
	return body
}

func projectReposMapToAPI(ctx context.Context, m types.Map) (map[string][]int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.IsNull() || m.IsUnknown() {
		return map[string][]int64{}, diags
	}

	result := make(map[string][]int64, len(m.Elements()))
	for projectID, value := range m.Elements() {
		set, ok := value.(types.Set)
		if !ok {
			diags.AddError(
				"Invalid project_repos_map",
				fmt.Sprintf("expected a set of repository IDs for project %q, got %T", projectID, value),
			)
			continue
		}

		var repoIDs []int64
		diags.Append(set.ElementsAs(ctx, &repoIDs, false)...)
		result[projectID] = helpers.NormalizeIDs(repoIDs)
	}

	return result, diags
}

func projectReposMapFromAPI(ctx context.Context, api map[string][]int64) (types.Map, diag.Diagnostics) {
	setType := types.SetType{ElemType: types.Int64Type}
	if api == nil {
		api = map[string][]int64{}
	}

	elems := make(map[string]attr.Value, len(api))
	var diags diag.Diagnostics
	for projectID, repoIDs := range api {
		set, setDiags := types.SetValueFrom(ctx, types.Int64Type, helpers.NormalizeIDs(repoIDs))
		diags.Append(setDiags...)
		if setDiags.HasError() {
			return types.MapNull(setType), diags
		}
		elems[projectID] = set
	}

	m, mapDiags := types.MapValue(setType, elems)
	diags.Append(mapDiags...)
	return m, diags
}

func emptyProjectKeys(ctx context.Context, m types.Map) (map[string]struct{}, diag.Diagnostics) {
	keys := make(map[string]struct{})
	api, diags := projectReposMapToAPI(ctx, m)
	if diags.HasError() {
		return keys, diags
	}
	for projectID, repoIDs := range api {
		if len(repoIDs) == 0 {
			keys[projectID] = struct{}{}
		}
	}
	return keys, diags
}

// normalizeProjectRepoMap drops empty projects that are not already in config,
// so unmapped tracker projects do not show up as drift.
func normalizeProjectRepoMap(api map[string][]int64, keepEmpty map[string]struct{}) map[string][]int64 {
	out := make(map[string][]int64)
	for projectID, repoIDs := range api {
		ids := helpers.NormalizeIDs(repoIDs)
		if len(ids) == 0 {
			if _, ok := keepEmpty[projectID]; !ok {
				continue
			}
		}
		out[projectID] = ids
	}
	return out
}

func missingRepoMappings(planned, actual map[string][]int64) map[string][]int64 {
	missing := make(map[string][]int64)
	for projectID, plannedIDs := range planned {
		dropped := helpers.DroppedRepoIDs(plannedIDs, actual[projectID])
		if len(dropped) > 0 {
			missing[projectID] = dropped
		}
	}
	return missing
}

func repoMapFromAPI(api projectMappingAPI) map[string][]int64 {
	if api.MappingMode != "" && api.MappingMode != projectRepoMapMode {
		return map[string][]int64{}
	}
	return api.ProjectRepoMap
}

func formatMissingMappings(missing map[string][]int64) string {
	projectIDs := make([]string, 0, len(missing))
	for projectID := range missing {
		projectIDs = append(projectIDs, projectID)
	}
	slices.Sort(projectIDs)

	parts := make([]string, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		parts = append(parts, fmt.Sprintf("project %s (missing repository IDs: %v)", projectID, missing[projectID]))
	}
	return strings.Join(parts, "; ")
}
