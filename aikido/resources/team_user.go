package resources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/teams"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/users"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &teamUserResource{}
	_ resource.ResourceWithImportState = &teamUserResource{}
	_ resource.ResourceWithConfigure   = &teamUserResource{}
)

func NewTeamUserResource() resource.Resource {
	return &teamUserResource{}
}

type teamUserResource struct {
	client *client.Client
}

// teamUserModel is the Terraform state. The membership itself has no ID in
// Aikido, so the two halves are composed into one.
type teamUserModel struct {
	ID     types.String `tfsdk:"id"`
	TeamID types.Int64  `tfsdk:"team_id"`
	UserID types.Int64  `tfsdk:"user_id"`
}

func (r *teamUserResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_team_user"
}

func (r *teamUserResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages one user's membership of a team created in Aikido. " +
			"The user must already exist in the workspace; this resource never invites, onboards or deactivates anyone. " +
			"Membership of teams synced from a Git provider belongs to that provider and is rejected: " +
			"add the user to the team in the Git provider instead, or use a separate Aikido team.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite identifier of the membership, team_id:user_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"team_id": schema.Int64Attribute{
				Required:    true,
				Description: "Aikido team ID.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.Int64Attribute{
				Required:    true,
				Description: "Aikido user ID. Use the aikido_users data source to resolve a person's email to this ID.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *teamUserResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *teamUserResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned teamUserModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diagnostics := r.createMembership(ctx, planned)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *teamUserResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState teamUserModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	teamID, userID := priorState.TeamID.ValueInt64(), priorState.UserID.ValueInt64()

	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		// A team removed outside Terraform takes its memberships with it.
		if client.NotInList(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading team membership", err.Error())
		return
	}

	response.Diagnostics.Append(importedTeamMembershipDiagnostics(team)...)
	if response.Diagnostics.HasError() {
		return
	}

	found, diagnostics := r.readMembership(ctx, teamID, userID)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}
	if !found {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, teamUserModel{
		ID:     types.StringValue(teamUserID(teamID, userID)),
		TeamID: types.Int64Value(teamID),
		UserID: types.Int64Value(userID),
	})...)
}

// Update never runs: both attributes require replacement. It exists because the
// resource interface demands it.
func (r *teamUserResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned teamUserModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, planned)...)
}

func (r *teamUserResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState teamUserModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(
		r.deleteMembership(ctx, priorState.TeamID.ValueInt64(), priorState.UserID.ValueInt64())...,
	)
}

// ImportState adopts an existing membership from a team_id:user_id pair. The
// passthrough helper cannot be used: it sets a single attribute, and this
// resource needs both halves of the identifier.
func (r *teamUserResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	teamID, userID, err := parseTeamUserID(request.ID)
	if err != nil {
		response.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), teamUserID(teamID, userID))...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("team_id"), teamID)...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
}

// createMembership adds the user to the team, refusing first if the team is
// owned by a Git provider.
func (r *teamUserResource) createMembership(ctx context.Context, planned teamUserModel) (teamUserModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	teamID, userID := planned.TeamID.ValueInt64(), planned.UserID.ValueInt64()

	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		diagnostics.AddError("Error reading team", err.Error())
		return teamUserModel{}, diagnostics
	}

	diagnostics.Append(importedTeamMembershipDiagnostics(team)...)
	if diagnostics.HasError() {
		return teamUserModel{}, diagnostics
	}

	if err := r.addMember(ctx, teamID, userID); err != nil {
		// The write may have committed before the response was lost. Adding
		// someone who is already a member succeeds, so the membership itself is
		// the authority on whether this failed.
		users.InvalidateTeam(r.client, teamID)
		found, readDiagnostics := r.readMembership(ctx, teamID, userID)
		if readDiagnostics.HasError() || !found {
			diagnostics.AddError(
				"Error adding user to team",
				fmt.Sprintf("%s\n\nCheck that team %d exists and that user %d belongs to this workspace.", err, teamID, userID),
			)
			return teamUserModel{}, diagnostics
		}
	}

	return teamUserModel{
		ID:     types.StringValue(teamUserID(teamID, userID)),
		TeamID: types.Int64Value(teamID),
		UserID: types.Int64Value(userID),
	}, diagnostics
}

// readMembership reports whether the user is a member of the team. Membership is
// read from the team's member list, which is cached per team, so many
// memberships across a few teams cost one request per team.
func (r *teamUserResource) readMembership(ctx context.Context, teamID, userID int64) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	members, err := users.InTeam(ctx, r.client, teamID)
	if err != nil {
		diagnostics.AddError("Error reading team members", err.Error())
		return false, diagnostics
	}

	for _, member := range members {
		// Deactivated members are still members, and the member list includes
		// them, so activation is deliberately not considered here.
		if member.ID == userID {
			return true, diagnostics
		}
	}

	return false, diagnostics
}

// deleteMembership removes the user from the team, refusing first if the team is
// owned by a Git provider. Aikido rejects that write as well; refusing here says
// why, rather than surfacing a bare API error in the middle of a destroy.
func (r *teamUserResource) deleteMembership(ctx context.Context, teamID, userID int64) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		// A team removed outside Terraform took its memberships with it.
		if client.NotInList(err) {
			return diagnostics
		}
		diagnostics.AddError("Error removing user from team", err.Error())
		return diagnostics
	}

	diagnostics.Append(importedTeamMembershipDiagnostics(team)...)
	if diagnostics.HasError() {
		return diagnostics
	}

	if err := r.removeMember(ctx, teamID, userID); err != nil && !client.NotFound(err) {
		diagnostics.AddError("Error removing user from team", err.Error())
	}

	return diagnostics
}

func (r *teamUserResource) addMember(ctx context.Context, teamID, userID int64) error {
	return r.writeMembership(ctx, teamID, userID, "addUser")
}

func (r *teamUserResource) removeMember(ctx context.Context, teamID, userID int64) error {
	return r.writeMembership(ctx, teamID, userID, "removeUser")
}

// writeMembership posts a membership change and drops the team's cached member
// list, so a read later in the same apply reflects the write.
func (r *teamUserResource) writeMembership(ctx context.Context, teamID, userID int64, action string) error {
	// Deferred: a call that fails may still have changed the membership.
	defer users.InvalidateTeam(r.client, teamID)

	endpoint := teams.BasePath + "/" + strconv.FormatInt(teamID, 10) + "/" + action

	return r.client.Do(ctx, http.MethodPost, endpoint, map[string]int64{"user_id": userID}, nil)
}

// importedTeamMembershipDiagnostics refuses memberships on teams a Git provider
// owns. Aikido rejects them too; failing here says why, and says it before a
// write is attempted.
func importedTeamMembershipDiagnostics(team teams.Team) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	if teams.IsImported(team) {
		diagnostics.AddError(
			"Team membership is managed by an external source",
			fmt.Sprintf("Team %q (ID %d) is synced from %s, which owns its membership. "+
				"Aikido rejects members added to it, and a synchronisation would remove them again. "+
				"Add the user to the team in %s, or create a separate Aikido team for the access you want to declare in Terraform.",
				team.Name, team.ID, team.ExternalSource, team.ExternalSource),
		)
	}

	return diagnostics
}

func teamUserID(teamID, userID int64) string {
	return strconv.FormatInt(teamID, 10) + ":" + strconv.FormatInt(userID, 10)
}

// parseTeamUserID splits a team_id:user_id identifier. Import is the only path
// that supplies one by hand, so the error states the expected format.
func parseTeamUserID(id string) (int64, int64, error) {
	invalid := fmt.Errorf("invalid membership id %q: expected team_id:user_id, for example 123:456", id)

	teamPart, userPart, found := strings.Cut(id, ":")
	if !found || strings.Contains(userPart, ":") {
		return 0, 0, invalid
	}

	teamID, teamErr := strconv.ParseInt(teamPart, 10, 64)
	userID, userErr := strconv.ParseInt(userPart, 10, 64)
	if teamErr != nil || userErr != nil {
		return 0, 0, invalid
	}

	return teamID, userID, nil
}
