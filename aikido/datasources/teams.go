package datasources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/teams"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &teamsDataSource{}
	_ datasource.DataSourceWithConfigure = &teamsDataSource{}
)

func NewTeamsDataSource() datasource.DataSource {
	return &teamsDataSource{}
}

type teamsDataSource struct {
	client *client.Client
}

// teamsDataSourceModel holds the optional filters plus the matched teams.
// Filters are applied in-process against the shared team list cache, so a
// configuration that also manages aikido_team resources pays for one paginated
// list overall rather than one per data source.
type teamsDataSourceModel struct {
	Name           types.String  `tfsdk:"name"`
	Active         types.Bool    `tfsdk:"active"`
	ExternalSource types.String  `tfsdk:"external_source"`
	Imported       types.Bool    `tfsdk:"imported"`
	IDs            []types.Int64 `tfsdk:"ids"`
	Teams          []teamModel   `tfsdk:"teams"`
}

type teamModel struct {
	ID               types.String  `tfsdk:"id"`
	Name             types.String  `tfsdk:"name"`
	Active           types.Bool    `tfsdk:"active"`
	Imported         types.Bool    `tfsdk:"imported"`
	ExternalSource   types.String  `tfsdk:"external_source"`
	ExternalSourceID types.String  `tfsdk:"external_source_id"`
	RepositoryIDs    []types.Int64 `tfsdk:"repository_ids"`
}

func (d *teamsDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_teams"
}

func (d *teamsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Looks up Aikido teams, both those created in Aikido and those synced from a Git provider. " +
			"Filters combine with AND; a filter that matches nothing yields an empty list rather than an error. " +
			"Teams synced from a Git provider cannot be managed by aikido_team, aikido_team_user or aikido_team_resource, but they are readable here: " +
			"use this data source to see which repositories they already cover, and to decide what an Aikido-managed team should cover on top.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Only return teams whose name is exactly this. Matching is exact, not a substring or glob.",
			},
			"active": schema.BoolAttribute{
				Optional:    true,
				Description: "Only return teams with this activation state. Omit to return both active and inactive teams.",
			},
			"external_source": schema.StringAttribute{
				Optional: true,
				Description: "Only return teams synced from this Git provider, for example github. " +
					"To select every synced team regardless of provider, use imported instead.",
			},
			"imported": schema.BoolAttribute{
				Optional: true,
				Description: "Only return synced teams (true) or only teams created in Aikido (false). " +
					"Omit to return both. Teams created in Aikido are the ones aikido_team can manage.",
			},
			"ids": schema.SetAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Numeric IDs of the matching teams, typed to match the team_id attribute of aikido_team_user.",
			},
			"teams": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Matching teams, ordered by Aikido team ID.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Aikido team ID, as a string to match the id attribute of aikido_team. For the numeric team_id attribute of aikido_team_user, use the ids attribute of this data source instead.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the team.",
						},
						"active": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the team is currently active in Aikido.",
						},
						"imported": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the team is synced from a Git provider, and therefore not manageable by aikido_team.",
						},
						"external_source": schema.StringAttribute{
							Computed:    true,
							Description: "Git provider the team is synced from, or an empty string for a team created in Aikido.",
						},
						"external_source_id": schema.StringAttribute{
							Computed:    true,
							Description: "Identifier of the team in the Git provider it is synced from, or an empty string.",
						},
						"repository_ids": schema.ListAttribute{
							Computed:    true,
							ElementType: types.Int64Type,
							Description: "Numeric IDs of the code repositories the team is responsible for, in ascending order. " +
								"Repositories currently deactivated in Aikido are not reported, and responsibilities other than " +
								"code repositories — clouds, container repositories, domains and Zen apps — are not listed here.",
						},
					},
				},
			},
		},
	}
}

func (d *teamsDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
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
	d.client = apiClient
}

func (d *teamsDataSource) Read(ctx context.Context, request datasource.ReadRequest, response *datasource.ReadResponse) {
	var config teamsDataSourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(unknownTeamFilterDiagnostics(config)...)
	if response.Diagnostics.HasError() {
		return
	}

	allTeams, err := teams.All(ctx, d.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading teams", err.Error())
		return
	}

	matched, matchedIDs := matchingTeams(allTeams, config)
	config.Teams = matched
	config.IDs = matchedIDs

	response.Diagnostics.Append(response.State.Set(ctx, &config)...)
}

// matchingTeams filters and maps in a single pass, so that ids stays aligned
// with teams entry for entry. Both are always non-nil, so that no match yields
// an empty list rather than null.
func matchingTeams(allTeams []teams.Team, config teamsDataSourceModel) ([]teamModel, []types.Int64) {
	matched := make([]teamModel, 0, len(allTeams))
	matchedIDs := make([]types.Int64, 0, len(allTeams))

	for _, apiTeam := range allTeams {
		if !teamMatchesFilters(apiTeam, config) {
			continue
		}
		matched = append(matched, teamModelFromAPI(apiTeam))
		matchedIDs = append(matchedIDs, types.Int64Value(apiTeam.ID))
	}

	return matched, matchedIDs
}

// unknownTeamFilterDiagnostics rejects filters that are not known at read time.
// Refusing beats ignoring: an ignored filter would widen the result to every
// team, and that result feeds team_id.
func unknownTeamFilterDiagnostics(config teamsDataSourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	filters := []struct {
		name      string
		isUnknown bool
	}{
		{"name", config.Name.IsUnknown()},
		{"active", config.Active.IsUnknown()},
		{"external_source", config.ExternalSource.IsUnknown()},
		{"imported", config.Imported.IsUnknown()},
	}

	for _, filter := range filters {
		if !filter.isUnknown {
			continue
		}
		diagnostics.AddAttributeError(
			path.Root(filter.name),
			"Unknown team filter",
			fmt.Sprintf("The %s filter is not known at read time, so it cannot be used to select teams. "+
				"Set it to a value that does not depend on an attribute that is only known after apply.", filter.name),
		)
	}

	return diagnostics
}

// teamMatchesFilters reports whether a team satisfies every set filter. Unset
// filters never exclude anything.
func teamMatchesFilters(apiTeam teams.Team, config teamsDataSourceModel) bool {
	if !config.Name.IsNull() && apiTeam.Name != config.Name.ValueString() {
		return false
	}
	if !config.Active.IsNull() && apiTeam.Active != config.Active.ValueBool() {
		return false
	}
	if !config.ExternalSource.IsNull() && apiTeam.ExternalSource != config.ExternalSource.ValueString() {
		return false
	}
	if !config.Imported.IsNull() && teams.IsImported(apiTeam) != config.Imported.ValueBool() {
		return false
	}

	return true
}

// teamModelFromAPI maps an API team into the data source model. Repository IDs
// come from teams.CodeRepoIDs, so they are sorted and carry only the
// responsibilities aikido_team can also express.
func teamModelFromAPI(apiTeam teams.Team) teamModel {
	repositoryIDs := make([]types.Int64, 0, len(apiTeam.Responsibilities))
	for _, id := range teams.CodeRepoIDs(apiTeam) {
		repositoryIDs = append(repositoryIDs, types.Int64Value(id))
	}

	return teamModel{
		ID:               types.StringValue(strconv.FormatInt(apiTeam.ID, 10)),
		Name:             types.StringValue(apiTeam.Name),
		Active:           types.BoolValue(apiTeam.Active),
		Imported:         types.BoolValue(teams.IsImported(apiTeam)),
		ExternalSource:   types.StringValue(apiTeam.ExternalSource),
		ExternalSourceID: types.StringValue(apiTeam.ExternalSourceID),
		RepositoryIDs:    repositoryIDs,
	}
}
