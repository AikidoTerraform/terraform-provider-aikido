package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/teams"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &teamResource{}
	_ resource.ResourceWithImportState = &teamResource{}
	_ resource.ResourceWithConfigure   = &teamResource{}
)

func NewTeamResource() resource.Resource {
	return &teamResource{}
}

type teamResource struct {
	client *client.Client
}

// teamModel is the Terraform state. The ID is a string by TF convention even
// though the API uses integers.
type teamModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	RepositoryIDs types.Set    `tfsdk:"repository_ids"`
	Active        types.Bool   `tfsdk:"active"`
}

func (r *teamResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_team"
}

func (r *teamResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages a team created in Aikido, and optionally the code repositories it is responsible for. " +
			"Teams synced from a Git provider belong to that provider and are rejected by this resource; " +
			"create a separate Aikido team instead of trying to manage an imported one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido team ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the team.",
			},
			"repository_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.Int64Type,
				Description: "Numeric IDs of the code repositories this team is responsible for. " +
					"When set, the list is authoritative: repositories removed from it are unlinked on the next apply, " +
					"and an empty set unlinks every repository. Omit the attribute to leave the team's responsibilities untouched. " +
					"Path limitations and responsibilities other than code repositories cannot be expressed here; " +
					"a team carrying either is rejected rather than silently stripped of them.",
			},
			"active": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team is currently active in Aikido.",
			},
		},
	}
}

func (r *teamResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *teamResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned teamModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diagnostics := r.createTeam(ctx, planned)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *teamResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState teamModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseTeamID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading team", err.Error())
		return
	}

	team, err := teams.ByID(ctx, r.client, id)
	if err != nil {
		if client.NotFound(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading team", err.Error())
		return
	}

	managesRepositories := !priorState.RepositoryIDs.IsNull()
	response.Diagnostics.Append(teamGuardDiagnostics(team, managesRepositories)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, teamModelFromAPI(team, managesRepositories))...)
}

func (r *teamResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned teamModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseTeamID(planned.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error updating team", err.Error())
		return
	}

	state, diagnostics := r.updateTeam(ctx, id, planned)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *teamResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState teamModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseTeamID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error deleting team", err.Error())
		return
	}

	team, err := teams.ByID(ctx, r.client, id)
	if err != nil {
		if client.NotFound(err) {
			return
		}
		response.Diagnostics.AddError("Error deleting team", err.Error())
		return
	}
	// Imported teams are rejected by the API too; failing here says why.
	response.Diagnostics.Append(teamGuardDiagnostics(team, false)...)
	if response.Diagnostics.HasError() {
		return
	}

	if err := teams.Delete(ctx, r.client, id); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deleting team", err.Error())
	}
}

// ImportState adopts an existing manual team by its numeric ID. repository_ids
// stays unmanaged until it is written into the configuration.
func (r *teamResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

// createTeam creates the team and, when repository_ids is set, writes its
// responsibilities. The create endpoint takes a name only, so responsibilities
// always need the second call.
func (r *teamResource) createTeam(ctx context.Context, planned teamModel) (teamModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	codeRepoIDs, conversionDiagnostics := repositoryIDsFilter(ctx, planned.RepositoryIDs)
	diagnostics.Append(conversionDiagnostics...)
	if diagnostics.HasError() {
		return teamModel{}, diagnostics
	}

	id, err := teams.Create(ctx, r.client, planned.Name.ValueString())
	if err != nil {
		diagnostics.AddError("Error creating team", err.Error())
		return teamModel{}, diagnostics
	}

	if codeRepoIDs != nil {
		if err := teams.Update(ctx, r.client, id, planned.Name.ValueString(), codeRepoIDs); err != nil {
			diagnostics.AddError("Error setting team responsibilities", err.Error())
			return teamModel{}, diagnostics
		}
	}

	return r.readBack(ctx, id, codeRepoIDs != nil, diagnostics)
}

// updateTeam renames the team and replaces its responsibilities when they are
// managed, refusing first if the write would destroy something it cannot express.
func (r *teamResource) updateTeam(ctx context.Context, id int64, planned teamModel) (teamModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	codeRepoIDs, conversionDiagnostics := repositoryIDsFilter(ctx, planned.RepositoryIDs)
	diagnostics.Append(conversionDiagnostics...)
	if diagnostics.HasError() {
		return teamModel{}, diagnostics
	}

	team, err := teams.ByID(ctx, r.client, id)
	if err != nil {
		diagnostics.AddError("Error updating team", err.Error())
		return teamModel{}, diagnostics
	}

	diagnostics.Append(teamGuardDiagnostics(team, codeRepoIDs != nil)...)
	if diagnostics.HasError() {
		return teamModel{}, diagnostics
	}

	if err := teams.Update(ctx, r.client, id, planned.Name.ValueString(), codeRepoIDs); err != nil {
		diagnostics.AddError("Error updating team", err.Error())
		return teamModel{}, diagnostics
	}

	return r.readBack(ctx, id, codeRepoIDs != nil, diagnostics)
}

// readBack re-reads the team after a write, so state holds what the API stored
// rather than what was planned.
func (r *teamResource) readBack(ctx context.Context, id int64, managesRepositories bool, diagnostics diag.Diagnostics) (teamModel, diag.Diagnostics) {
	team, err := teams.ByID(ctx, r.client, id)
	if err != nil {
		diagnostics.AddError("Error reading team back", err.Error())
		return teamModel{}, diagnostics
	}

	return teamModelFromAPI(team, managesRepositories), diagnostics
}

// repositoryIDsFilter converts repository_ids into the value teams.Update
// expects: nil leaves responsibilities untouched, while a non-nil slice replaces
// them, so a null set and an empty one must stay distinguishable.
func repositoryIDsFilter(ctx context.Context, repositoryIDs types.Set) (*[]int64, diag.Diagnostics) {
	if repositoryIDs.IsNull() || repositoryIDs.IsUnknown() {
		return nil, nil
	}

	ids := []int64{}
	diagnostics := repositoryIDs.ElementsAs(ctx, &ids, false)

	return &ids, diagnostics
}

// teamGuardDiagnostics refuses the writes this resource cannot perform safely:
// anything at all on an imported team, and a responsibilities replace that would
// drop resources the update endpoint cannot express.
func teamGuardDiagnostics(team teams.Team, managesRepositories bool) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	if teams.IsImported(team) {
		diagnostics.AddError(
			"Team is managed by an external source",
			fmt.Sprintf("Team %q (ID %d) is synced from %s, which owns its name, members and repositories. "+
				"Aikido rejects changes to it, so aikido_team cannot manage it. "+
				"Create a separate Aikido team for the responsibilities you want to declare in Terraform.",
				team.Name, team.ID, team.ExternalSource),
		)
	}

	if managesRepositories {
		if unrepresentable := teams.Unrepresentable(team); len(unrepresentable) > 0 {
			diagnostics.AddError(
				"Team has responsibilities this resource cannot manage",
				fmt.Sprintf("Team %q (ID %d) is responsible for %s. "+
					"Writing repository_ids replaces the team's whole responsibility list, and the Aikido API accepts only "+
					"code repositories without path limitations, so those would be removed. "+
					"Remove repository_ids from this resource to leave the team's responsibilities untouched, "+
					"or clear the unsupported responsibilities in Aikido first.",
					team.Name, team.ID, describeResponsibilities(unrepresentable)),
			)
		}
	}

	return diagnostics
}

// describeResponsibilities names what a replace would destroy, so the error says
// which resource to look at rather than just that one exists.
func describeResponsibilities(responsibilities []teams.Responsibility) string {
	descriptions := make([]string, 0, len(responsibilities))
	for _, responsibility := range responsibilities {
		description := fmt.Sprintf("%s %d", responsibility.Type, responsibility.ID)
		if paths := append(append([]string{}, responsibility.IncludedPaths...), responsibility.ExcludedPaths...); len(paths) > 0 {
			description += " (paths " + strings.Join(paths, ", ") + ")"
		}
		descriptions = append(descriptions, description)
	}

	return strings.Join(descriptions, ", ")
}

func parseTeamID(teamID string) (int64, error) {
	id, err := strconv.ParseInt(teamID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid team id %q: %w", teamID, err)
	}

	return id, nil
}

// teamModelFromAPI maps an API team into Terraform state. repository_ids stays
// null when the configuration does not manage it.
func teamModelFromAPI(team teams.Team, managesRepositories bool) teamModel {
	state := teamModel{
		ID:            types.StringValue(strconv.FormatInt(team.ID, 10)),
		Name:          types.StringValue(team.Name),
		Active:        types.BoolValue(team.Active),
		RepositoryIDs: types.SetNull(types.Int64Type),
	}

	if managesRepositories {
		elements := make([]attr.Value, 0, len(team.Responsibilities))
		for _, id := range teams.CodeRepoIDs(team) {
			elements = append(elements, types.Int64Value(id))
		}
		state.RepositoryIDs = types.SetValueMust(types.Int64Type, elements)
	}

	return state
}
