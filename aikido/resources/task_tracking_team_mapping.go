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
	mapTeamsToProjectsPath            = "/public/v1/task_tracking/mapTeamsToProjects"
	taskTrackingTeamMappingResourceID = "task_tracking_team_mapping"
	projectTeamsMapAttribute          = "project_teams_map"
)

var (
	_ resource.Resource                = &taskTrackingTeamMappingResource{}
	_ resource.ResourceWithImportState = &taskTrackingTeamMappingResource{}
	_ resource.ResourceWithConfigure   = &taskTrackingTeamMappingResource{}
)

func NewTaskTrackingTeamMappingResource() resource.Resource {
	return &taskTrackingTeamMappingResource{}
}

type taskTrackingTeamMappingResource struct {
	client *client.Client
}

type taskTrackingTeamMappingModel struct {
	ID              types.String `tfsdk:"id"`
	IntegrationID   types.Int64  `tfsdk:"integration_id"`
	ProjectTeamsMap types.Map    `tfsdk:"project_teams_map"`
}

func (r *taskTrackingTeamMappingResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_task_tracking_team_mapping"
}

func (r *taskTrackingTeamMappingResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Maps Aikido teams to task-tracker projects (Linear, Jira, and others). " +
			"This is team mapping, not code-repository mapping. " +
			"Define one resource per task-tracker integration. Omit integration_id when the workspace has a single integration. " +
			"The Aikido API has no delete endpoint for this mapping, so destroying this resource " +
			"only removes it from Terraform state and leaves the remote mapping unchanged.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Task tracking team-to-project mapping identifier. The integration ID when set, otherwise a workspace-wide sentinel.",
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
			"project_teams_map": schema.MapAttribute{
				Required:    true,
				ElementType: types.SetType{ElemType: types.Int64Type},
				Description: "Map of task-tracker project IDs to Aikido team IDs. " +
					"Project IDs are those of the connected tracker (for example Linear team IDs or Jira project IDs). " +
					"An empty team set unmaps that project. " +
					"Projects omitted from the map are left unmapped in config; extra mapped projects in Aikido show as drift.",
			},
		},
	}
}

func (r *taskTrackingTeamMappingResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *taskTrackingTeamMappingResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned taskTrackingTeamMappingModel
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

func (r *taskTrackingTeamMappingResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState taskTrackingTeamMappingModel
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

func (r *taskTrackingTeamMappingResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned taskTrackingTeamMappingModel
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
func (r *taskTrackingTeamMappingResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *taskTrackingTeamMappingResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
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

func (r *taskTrackingTeamMappingResource) applyMapping(ctx context.Context, planned taskTrackingTeamMappingModel) (taskTrackingTeamMappingModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	plannedMap, mapDiags := idMapToAPI(ctx, planned.ProjectTeamsMap, projectTeamsMapAttribute)
	diags.Append(mapDiags...)
	if diags.HasError() {
		return taskTrackingTeamMappingModel{}, diags
	}

	if err := r.client.Do(ctx, "POST", mapTeamsToProjectsPath, constructMappingBody(projectTeamsMapAttribute, plannedMap, planned.IntegrationID), nil); err != nil {
		diags.AddError("Error configuring task tracking team mapping", err.Error())
		return taskTrackingTeamMappingModel{}, diags
	}

	actual, err := getProjectMapping(ctx, r.client, planned.IntegrationID)
	if err != nil {
		diags.AddError("Error reading task tracking team mapping", err.Error())
		return taskTrackingTeamMappingModel{}, diags
	}

	if missing := missingIDMappings(plannedMap, teamsMapFromAPI(actual)); len(missing) > 0 {
		diags.AddError(
			"Error configuring task tracking team mapping",
			fmt.Sprintf(
				"Aikido did not persist the team mapping for: %s. "+
					"Check that the project IDs belong to the task tracker integration and that the team IDs exist. "+
					"If the workspace uses repository mapping, applying this resource switches it to team mapping.",
				formatMissingMappings(missing, "team"),
			),
		)
		return taskTrackingTeamMappingModel{}, diags
	}

	state := planned
	state.ID = types.StringValue(mappingResourceID(planned.IntegrationID))
	return state, diags
}

func (r *taskTrackingTeamMappingResource) readMapping(ctx context.Context, prior taskTrackingTeamMappingModel) (*taskTrackingTeamMappingModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	keepEmpty, keyDiags := emptyMapKeys(ctx, prior.ProjectTeamsMap, projectTeamsMapAttribute)
	diags.Append(keyDiags...)
	if diags.HasError() {
		return nil, diags
	}

	api, err := getProjectMapping(ctx, r.client, prior.IntegrationID)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}
		diags.AddError("Error reading task tracking team mapping", err.Error())
		return nil, diags
	}

	tfMap, mapDiags := idMapFromAPI(ctx, normalizeIDMap(teamsMapFromAPI(api), keepEmpty))
	diags.Append(mapDiags...)
	if diags.HasError() {
		return nil, diags
	}

	state := prior
	state.ID = types.StringValue(mappingResourceID(prior.IntegrationID))
	state.ProjectTeamsMap = tfMap
	return &state, diags
}
