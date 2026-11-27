package resources

import (
	"context"
	"fmt"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	taskTrackingCodeRepoMappingResourceID = "task_tracking_code_repo_mapping"
	projectReposMapAttribute              = "project_repos_map"
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
	resourceID, integrationID, err := parseMappingImportID(request.ID, taskTrackingCodeRepoMappingResourceID)
	if err != nil {
		response.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), resourceID)...)
	if !integrationID.IsNull() {
		response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("integration_id"), integrationID)...)
	}
}

func (r *taskTrackingCodeRepoMappingResource) applyMapping(ctx context.Context, planned taskTrackingCodeRepoMappingModel) (taskTrackingCodeRepoMappingModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	plannedMap, mapDiags := idMapToAPI(ctx, planned.ProjectReposMap, projectReposMapAttribute)
	diags.Append(mapDiags...)
	if diags.HasError() {
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	if err := r.client.Do(ctx, "POST", mapCodeReposToProjectsPath, constructMappingBody(projectReposMapAttribute, plannedMap, planned.IntegrationID), nil); err != nil {
		diags.AddError("Error configuring task tracking code repo mapping", err.Error())
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	actual, err := getProjectMapping(ctx, r.client, planned.IntegrationID)
	if err != nil {
		diags.AddError("Error reading task tracking code repo mapping", err.Error())
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	if missing := missingIDMappings(plannedMap, repoMapFromAPI(actual)); len(missing) > 0 {
		diags.AddError(
			"Error configuring task tracking code repo mapping",
			fmt.Sprintf(
				"Aikido did not persist the repository mapping for: %s. "+
					"Check that the project IDs belong to the task tracker integration and that the repository IDs exist. "+
					"If the workspace uses team mapping, applying this resource switches it to repository mapping.",
				formatMissingMappings(missing, "repository"),
			),
		)
		return taskTrackingCodeRepoMappingModel{}, diags
	}

	state := planned
	state.ID = types.StringValue(mappingResourceID(planned.IntegrationID, taskTrackingCodeRepoMappingResourceID))
	return state, diags
}

func (r *taskTrackingCodeRepoMappingResource) readMapping(ctx context.Context, prior taskTrackingCodeRepoMappingModel) (*taskTrackingCodeRepoMappingModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	keepEmpty, keyDiags := emptyMapKeys(ctx, prior.ProjectReposMap, projectReposMapAttribute)
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

	tfMap, mapDiags := idMapFromAPI(ctx, normalizeIDMap(repoMapFromAPI(api), keepEmpty))
	diags.Append(mapDiags...)
	if diags.HasError() {
		return nil, diags
	}

	state := prior
	state.ID = types.StringValue(mappingResourceID(prior.IntegrationID, taskTrackingCodeRepoMappingResourceID))
	state.ProjectReposMap = tfMap
	return &state, diags
}

// repoMapFromAPI returns the repo-to-project map, or an empty map when the
// integration is in team mapping mode.
func repoMapFromAPI(api projectMappingAPI) map[string][]int64 {
	if api.MappingMode != "" && api.MappingMode != mappingModeRepos {
		return map[string][]int64{}
	}
	return api.ProjectRepoMap
}
