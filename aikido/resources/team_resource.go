package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/teams"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

var (
	_ resource.Resource                = &teamLinkedResource{}
	_ resource.ResourceWithImportState = &teamLinkedResource{}
	_ resource.ResourceWithConfigure   = &teamLinkedResource{}
)

func NewTeamLinkedResource() resource.Resource {
	return &teamLinkedResource{}
}

type teamLinkedResource struct {
	client *client.Client
}

// teamLinkedModel is the Terraform state. The link itself has no ID in Aikido,
// so the team, the kind of resource and its ID are composed into one.
type teamLinkedModel struct {
	ID                 types.String             `tfsdk:"id"`
	TeamID             types.Int64              `tfsdk:"team_id"`
	RepoID             types.Int64              `tfsdk:"repo_id"`
	CloudID            types.Int64              `tfsdk:"cloud_id"`
	ImageID            types.Int64              `tfsdk:"image_id"`
	DomainID           types.Int64              `tfsdk:"domain_id"`
	ZenAppID           types.Int64              `tfsdk:"zen_app_id"`
	RepoPathLimitation *repoPathLimitationModel `tfsdk:"repo_path_limitation"`
}

type repoPathLimitationModel struct {
	LimitationType types.String `tfsdk:"limitation_type"`
	Paths          types.List   `tfsdk:"paths"`
}

func (r *teamLinkedResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_team_resource"
}

func (r *teamLinkedResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	idExactlyOneOf := []validator.Int64{
		int64validator.ExactlyOneOf(
			path.MatchRoot("repo_id"),
			path.MatchRoot("cloud_id"),
			path.MatchRoot("image_id"),
			path.MatchRoot("domain_id"),
			path.MatchRoot("zen_app_id"),
		),
	}
	replaceOnChange := []planmodifier.Int64{
		int64planmodifier.RequiresReplace(),
	}

	response.Schema = schema.Schema{
		Description: "Links one resource to a team created in Aikido: a code repository, cloud, container image, domain or Zen app. " +
			"A code repository can optionally be limited to included or excluded paths. " +
			"Teams synced from a Git provider belong to that provider and are rejected; " +
			"create a separate Aikido team instead of trying to link resources to an imported one. " +
			"Do not manage the same code repository with both this resource and aikido_team.repository_ids: " +
			"that attribute replaces the team's whole repository set and would unlink this resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite identifier of the link, team_id:kind:resource_id. Kind is one of repo, cloud, image, domain, zen_app.",
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
			"repo_id": schema.Int64Attribute{
				Optional:      true,
				Description:   "Aikido code repository ID to link. Mutually exclusive with cloud_id, image_id, domain_id and zen_app_id.",
				Validators:    idExactlyOneOf,
				PlanModifiers: replaceOnChange,
			},
			"cloud_id": schema.Int64Attribute{
				Optional:      true,
				Description:   "Aikido cloud ID to link. Mutually exclusive with the other resource ID attributes.",
				Validators:    idExactlyOneOf,
				PlanModifiers: replaceOnChange,
			},
			"image_id": schema.Int64Attribute{
				Optional:      true,
				Description:   "Aikido container image ID to link. Mutually exclusive with the other resource ID attributes.",
				Validators:    idExactlyOneOf,
				PlanModifiers: replaceOnChange,
			},
			"domain_id": schema.Int64Attribute{
				Optional:      true,
				Description:   "Aikido domain ID to link. Mutually exclusive with the other resource ID attributes.",
				Validators:    idExactlyOneOf,
				PlanModifiers: replaceOnChange,
			},
			"zen_app_id": schema.Int64Attribute{
				Optional:      true,
				Description:   "Aikido Zen app ID to link. Mutually exclusive with the other resource ID attributes.",
				Validators:    idExactlyOneOf,
				PlanModifiers: replaceOnChange,
			},
			"repo_path_limitation": schema.SingleNestedAttribute{
				Optional: true,
				Description: "Limits a linked code repository to certain paths. " +
					"Only valid with repo_id. Omit it to cover the whole repository; removing it from the configuration clears any existing filter.",
				Validators: []validator.Object{
					objectvalidator.AlsoRequires(path.MatchRoot("repo_id")),
				},
				Attributes: map[string]schema.Attribute{
					"limitation_type": schema.StringAttribute{
						Required:    true,
						Description: "Whether paths are included or excluded. One of include, exclude.",
						Validators: []validator.String{
							stringvalidator.OneOf(teams.LimitationInclude, teams.LimitationExclude),
						},
					},
					"paths": schema.ListAttribute{
						Required:    true,
						ElementType: types.StringType,
						Description: "Repository paths to include or exclude, for example /client/.",
						Validators: []validator.List{
							listvalidator.SizeAtLeast(1),
						},
					},
				},
			},
		},
	}
}

func (r *teamLinkedResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *teamLinkedResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned teamLinkedModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diagnostics := r.createLink(ctx, planned)
	response.Diagnostics.Append(diagnostics...)

	// Save a link that was created even when reading it back failed.
	// Terraform keeps the state a failed Create returns, and dropping it here
	// would leave a real link untracked.
	if state.ID.IsNull() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *teamLinkedResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState teamLinkedModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	linked, diagnostics := linkedFromModel(priorState)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}

	teamID := priorState.TeamID.ValueInt64()
	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		if client.NotInList(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading team resource link", err.Error())
		return
	}

	response.Diagnostics.Append(importedTeamResourceDiagnostics(team)...)
	if response.Diagnostics.HasError() {
		return
	}

	responsibility, found := teams.FindResponsibility(team, linked.Kind, linked.ID)
	if !found {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, teamLinkedModelFromAPI(teamID, linked, responsibility))...)
}

func (r *teamLinkedResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned, priorState teamLinkedModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diagnostics := r.updateLink(ctx, planned, priorState)
	response.Diagnostics.Append(diagnostics...)

	// Save a link that was updated even when reading it back failed.
	// Terraform keeps the prior state a failed Update returns, and dropping
	// the written values here would leave Aikido and state out of sync.
	if state.ID.IsNull() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *teamLinkedResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState teamLinkedModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(r.deleteLink(ctx, priorState)...)
}

func (r *teamLinkedResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	teamID, kind, resourceID, err := parseTeamResourceID(request.ID)
	if err != nil {
		response.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), teamResourceID(teamID, kind, resourceID))...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("team_id"), teamID)...)

	field, ok := kindAttribute(kind)
	if !ok {
		response.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("unknown resource kind %q", kind))
		return
	}
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root(field), resourceID)...)
}

func (r *teamLinkedResource) createLink(ctx context.Context, planned teamLinkedModel) (teamLinkedModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	linked, conversionDiagnostics := linkedFromModel(planned)
	diagnostics.Append(conversionDiagnostics...)
	limitation, conversionDiagnostics := pathLimitationFromModel(ctx, planned.RepoPathLimitation)
	diagnostics.Append(conversionDiagnostics...)
	if diagnostics.HasError() {
		return teamLinkedModel{}, diagnostics
	}

	teamID := planned.TeamID.ValueInt64()
	// Drop the cache so alreadyLinked is taken from a live list. A stale miss
	// would treat an existing link as this create when the write is later rejected.
	teams.InvalidateCache(r.client)
	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		diagnostics.AddError("Error reading team", err.Error())
		return teamLinkedModel{}, diagnostics
	}

	diagnostics.Append(importedTeamResourceDiagnostics(team)...)
	if diagnostics.HasError() {
		return teamLinkedModel{}, diagnostics
	}

	_, alreadyLinked := teams.FindResponsibility(team, linked.Kind, linked.ID)

	if err := teams.Link(ctx, r.client, teamID, linked, limitation); err != nil {
		teams.InvalidateCache(r.client)
		// A rejected link may still have landed. Only a responsibility that was
		// absent beforehand proves that it did: one the team already had says
		// nothing about this write, and accepting it would report a path
		// limitation as applied when the API never took it.
		if _, found := r.lookup(ctx, teamID, linked); found && !alreadyLinked {
			return r.readBack(ctx, teamID, linked, diagnostics)
		}

		hint := fmt.Sprintf("Check that team %d exists and that the %s %d belongs to this workspace.", teamID, linked.Kind, linked.ID)
		if alreadyLinked {
			hint = fmt.Sprintf(
				"Team %d was already responsible for %s %d before this apply, so nothing in this resource was applied. "+
					"Import the existing link with %q instead of creating it, and check that no aikido_team.repository_ids manages the same resource.",
				teamID, linked.Kind, linked.ID, teamResourceID(teamID, linked.Kind, linked.ID),
			)
		}
		diagnostics.AddError("Error linking resource to team", fmt.Sprintf("%s\n\n%s", err, hint))
		return teamLinkedModel{}, diagnostics
	}

	return r.readBack(ctx, teamID, linked, diagnostics)
}

func (r *teamLinkedResource) updateLink(ctx context.Context, planned, priorState teamLinkedModel) (teamLinkedModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	linked, conversionDiagnostics := linkedFromModel(planned)
	diagnostics.Append(conversionDiagnostics...)
	if diagnostics.HasError() {
		return teamLinkedModel{}, diagnostics
	}

	teamID := planned.TeamID.ValueInt64()
	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		diagnostics.AddError("Error updating team resource link", err.Error())
		return teamLinkedModel{}, diagnostics
	}

	diagnostics.Append(importedTeamResourceDiagnostics(team)...)
	if diagnostics.HasError() {
		return teamLinkedModel{}, diagnostics
	}

	limitation, conversionDiagnostics := pathLimitationFromModel(ctx, planned.RepoPathLimitation)
	diagnostics.Append(conversionDiagnostics...)
	if diagnostics.HasError() {
		return teamLinkedModel{}, diagnostics
	}

	if linked.Kind == teams.KindRepo {
		updated := teams.PathLimitation{Type: limitationTypeForClear(priorState), Paths: []string{}}
		if limitation != nil {
			updated = *limitation
		}
		if err := teams.UpdatePathLimitation(ctx, r.client, teamID, linked.ID, updated); err != nil {
			diagnostics.AddError("Error updating repository path limitation", err.Error())
			return teamLinkedModel{}, diagnostics
		}
	}

	return r.readBack(ctx, teamID, linked, diagnostics)
}

func (r *teamLinkedResource) deleteLink(ctx context.Context, priorState teamLinkedModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	linked, conversionDiagnostics := linkedFromModel(priorState)
	diagnostics.Append(conversionDiagnostics...)
	if diagnostics.HasError() {
		return diagnostics
	}

	teamID := priorState.TeamID.ValueInt64()
	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		if client.NotInList(err) {
			return diagnostics
		}
		diagnostics.AddError("Error unlinking resource from team", err.Error())
		return diagnostics
	}

	diagnostics.Append(importedTeamResourceDiagnostics(team)...)
	if diagnostics.HasError() {
		return diagnostics
	}

	if err := teams.Unlink(ctx, r.client, teamID, linked); err != nil && !client.NotFound(err) {
		diagnostics.AddError("Error unlinking resource from team", err.Error())
	}

	return diagnostics
}

func (r *teamLinkedResource) lookup(ctx context.Context, teamID int64, linked teams.LinkedResource) (teams.Responsibility, bool) {
	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		return teams.Responsibility{}, false
	}
	return teams.FindResponsibility(team, linked.Kind, linked.ID)
}

func (r *teamLinkedResource) readBack(ctx context.Context, teamID int64, linked teams.LinkedResource, diagnostics diag.Diagnostics) (teamLinkedModel, diag.Diagnostics) {
	team, err := teams.ByID(ctx, r.client, teamID)
	if err != nil {
		diagnostics.AddError("Error reading team resource link back", err.Error())
		return identityOnlyLinkedModel(teamID, linked), diagnostics
	}

	responsibility, found := teams.FindResponsibility(team, linked.Kind, linked.ID)
	if !found {
		diagnostics.AddError(
			"Error reading team resource link back",
			fmt.Sprintf("Team %d has no %s %d after the write.", teamID, linked.Kind, linked.ID),
		)
		return identityOnlyLinkedModel(teamID, linked), diagnostics
	}

	return teamLinkedModelFromAPI(teamID, linked, responsibility), diagnostics
}

func identityOnlyLinkedModel(teamID int64, linked teams.LinkedResource) teamLinkedModel {
	model := teamLinkedModel{
		ID:     types.StringValue(teamResourceID(teamID, linked.Kind, linked.ID)),
		TeamID: types.Int64Value(teamID),
	}
	setKindID(&model, linked)
	return model
}

func teamLinkedModelFromAPI(teamID int64, linked teams.LinkedResource, responsibility teams.Responsibility) teamLinkedModel {
	model := teamLinkedModel{
		ID:                 types.StringValue(teamResourceID(teamID, linked.Kind, linked.ID)),
		TeamID:             types.Int64Value(teamID),
		RepoPathLimitation: pathLimitationToModel(teams.PathLimitationOf(responsibility)),
	}
	setKindID(&model, linked)
	return model
}

func setKindID(model *teamLinkedModel, linked teams.LinkedResource) {
	id := types.Int64Value(linked.ID)
	switch linked.Kind {
	case teams.KindRepo:
		model.RepoID = id
	case teams.KindCloud:
		model.CloudID = id
	case teams.KindImage:
		model.ImageID = id
	case teams.KindDomain:
		model.DomainID = id
	case teams.KindZenApp:
		model.ZenAppID = id
	}
}

func linkedFromModel(model teamLinkedModel) (teams.LinkedResource, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	switch {
	case !model.RepoID.IsNull() && !model.RepoID.IsUnknown():
		return teams.LinkedResource{Kind: teams.KindRepo, ID: model.RepoID.ValueInt64()}, diagnostics
	case !model.CloudID.IsNull() && !model.CloudID.IsUnknown():
		return teams.LinkedResource{Kind: teams.KindCloud, ID: model.CloudID.ValueInt64()}, diagnostics
	case !model.ImageID.IsNull() && !model.ImageID.IsUnknown():
		return teams.LinkedResource{Kind: teams.KindImage, ID: model.ImageID.ValueInt64()}, diagnostics
	case !model.DomainID.IsNull() && !model.DomainID.IsUnknown():
		return teams.LinkedResource{Kind: teams.KindDomain, ID: model.DomainID.ValueInt64()}, diagnostics
	case !model.ZenAppID.IsNull() && !model.ZenAppID.IsUnknown():
		return teams.LinkedResource{Kind: teams.KindZenApp, ID: model.ZenAppID.ValueInt64()}, diagnostics
	}

	diagnostics.AddError("Missing resource ID", "Set exactly one of repo_id, cloud_id, image_id, domain_id or zen_app_id.")
	return teams.LinkedResource{}, diagnostics
}

func pathLimitationFromModel(ctx context.Context, model *repoPathLimitationModel) (*teams.PathLimitation, diag.Diagnostics) {
	if model == nil {
		return nil, nil
	}

	var paths []string
	diagnostics := model.Paths.ElementsAs(ctx, &paths, false)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	return &teams.PathLimitation{
		Type:  model.LimitationType.ValueString(),
		Paths: paths,
	}, diagnostics
}

func pathLimitationToModel(limitation *teams.PathLimitation) *repoPathLimitationModel {
	if limitation == nil || len(limitation.Paths) == 0 {
		return nil
	}

	elements := make([]attr.Value, 0, len(limitation.Paths))
	for _, pathValue := range limitation.Paths {
		elements = append(elements, types.StringValue(pathValue))
	}

	return &repoPathLimitationModel{
		LimitationType: types.StringValue(limitation.Type),
		Paths:          types.ListValueMust(types.StringType, elements),
	}
}

func limitationTypeForClear(priorState teamLinkedModel) string {
	if priorState.RepoPathLimitation != nil && !priorState.RepoPathLimitation.LimitationType.IsNull() {
		return priorState.RepoPathLimitation.LimitationType.ValueString()
	}
	return teams.LimitationInclude
}

func importedTeamResourceDiagnostics(team teams.Team) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	if teams.IsImported(team) {
		diagnostics.AddError(
			"Team resources are managed by an external source",
			fmt.Sprintf("Team %q (ID %d) is synced from %s, which owns the resources it is responsible for. "+
				"Aikido rejects resources linked to it, and a synchronisation would remove them again. "+
				"Link the resource in %s, or create a separate Aikido team for the responsibilities you want to declare in Terraform.",
				team.Name, team.ID, team.ExternalSource, team.ExternalSource),
		)
	}

	return diagnostics
}

func kindAttribute(kind string) (string, bool) {
	switch kind {
	case teams.KindRepo:
		return "repo_id", true
	case teams.KindCloud:
		return "cloud_id", true
	case teams.KindImage:
		return "image_id", true
	case teams.KindDomain:
		return "domain_id", true
	case teams.KindZenApp:
		return "zen_app_id", true
	default:
		return "", false
	}
}

func teamResourceID(teamID int64, kind string, resourceID int64) string {
	return strconv.FormatInt(teamID, 10) + ":" + kind + ":" + strconv.FormatInt(resourceID, 10)
}

func parseTeamResourceID(id string) (int64, string, int64, error) {
	invalid := fmt.Errorf("invalid team resource id %q: expected team_id:kind:resource_id, for example 123:repo:4 or 123:cloud:12", id)

	teamPart, rest, found := strings.Cut(id, ":")
	if !found {
		return 0, "", 0, invalid
	}
	kind, resourcePart, found := strings.Cut(rest, ":")
	if !found || strings.Contains(resourcePart, ":") || !teams.KnownKind(kind) {
		return 0, "", 0, invalid
	}

	teamID, teamErr := strconv.ParseInt(teamPart, 10, 64)
	resourceID, resourceErr := strconv.ParseInt(resourcePart, 10, 64)
	if teamErr != nil || resourceErr != nil {
		return 0, "", 0, invalid
	}

	return teamID, kind, resourceID, nil
}
