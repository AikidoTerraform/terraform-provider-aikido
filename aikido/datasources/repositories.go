// Package datasources contains the read-only Aikido data sources.
package datasources

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &repositoriesDataSource{}
	_ datasource.DataSourceWithConfigure = &repositoriesDataSource{}
)

func NewRepositoriesDataSource() datasource.DataSource {
	return &repositoriesDataSource{}
}

type repositoriesDataSource struct {
	client *client.Client
}

// repositoriesDataSourceModel holds the optional filters plus the matched
// repositories. Filters are applied in-process against the shared repository
// list cache, so a config that also manages aikido_repository resources pays
// for one paginated list overall rather than one per data source.
type repositoriesDataSourceModel struct {
	Name         types.String      `tfsdk:"name"`
	Branch       types.String      `tfsdk:"branch"`
	Active       types.Bool        `tfsdk:"active"`
	IDs          []types.Int64     `tfsdk:"ids"`
	Repositories []repositoryModel `tfsdk:"repositories"`
}

type repositoryModel struct {
	ID                    types.String   `tfsdk:"id"`
	Name                  types.String   `tfsdk:"name"`
	GitProvider           types.String   `tfsdk:"git_provider"`
	ExternalRepoID        types.String   `tfsdk:"external_repo_id"`
	ExternalRepoNumericID types.Int64    `tfsdk:"external_repo_numeric_id"`
	Active                types.Bool     `tfsdk:"active"`
	Branch                types.String   `tfsdk:"branch"`
	URL                   types.String   `tfsdk:"url"`
	LastScannedAt         types.Int64    `tfsdk:"last_scanned_at"`
	Connectivity          types.String   `tfsdk:"connectivity"`
	Sensitivity           types.String   `tfsdk:"sensitivity"`
	Labels                []types.String `tfsdk:"labels"`
}

func (d *repositoriesDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_repositories"
}

func (d *repositoriesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Looks up Aikido code repositories, so configuration can reference repositories by name " +
			"instead of hard-coding Aikido's numeric repository IDs. " +
			"Returns every repository, active and inactive, unless filters narrow the result. " +
			"Filters combine with AND; a filter that matches nothing yields an empty list rather than an error. " +
			"Use the ids attribute to feed the numeric repo_ids and code_repo_id attributes of the other resources, " +
			"and the repositories attribute when Terraform expressions need to select by naming convention.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional: true,
				Description: "Only return the repository whose name is exactly this. " +
					"Matching is exact, not a substring or glob. " +
					"To select repositories by naming convention instead, omit this and filter the repositories list with a Terraform expression, " +
					"for example: [for repository in data.aikido_repositories.all.repositories : tonumber(repository.id) if startswith(repository.name, \"team-a-\")].",
			},
			"branch": schema.StringAttribute{
				Optional:    true,
				Description: "Only return repositories whose scanned branch is exactly this.",
			},
			"active": schema.BoolAttribute{
				Optional:    true,
				Description: "Only return repositories with this activation state. Omit to return both active and inactive repositories.",
			},
			"ids": schema.SetAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Numeric IDs of the matching repositories. Typed to match the repo_ids attribute of the autofix settings resources, so it can be assigned to them directly; use one(...) to feed a single numeric code_repo_id.",
			},
			"repositories": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Matching repositories, ordered by Aikido repository ID.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Aikido code repository ID, as a string to match the id attribute of aikido_repository. For the numeric code_repo_id and repo_ids attributes, use the ids attribute of this data source instead.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the code repository.",
						},
						"git_provider": schema.StringAttribute{
							Computed:    true,
							Description: "Git provider hosting the repository.",
						},
						"external_repo_id": schema.StringAttribute{
							Computed:    true,
							Description: "Repository ID from the Git provider.",
						},
						"external_repo_numeric_id": schema.Int64Attribute{
							Computed:    true,
							Description: "Numeric repository ID from the Git provider.",
						},
						"active": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the repository is activated for scanning in Aikido.",
						},
						"branch": schema.StringAttribute{
							Computed:    true,
							Description: "Branch configured for scanning.",
						},
						"url": schema.StringAttribute{
							Computed:    true,
							Description: "External URL of the repository.",
						},
						"last_scanned_at": schema.Int64Attribute{
							Computed:    true,
							Description: "Unix timestamp of the last completed scan, or -1 if the repository has never been scanned.",
						},
						"connectivity": schema.StringAttribute{
							Computed:    true,
							Description: "Whether the code runs on an internet-connected server. One of: connected, not_connected, unknown.",
						},
						"sensitivity": schema.StringAttribute{
							Computed:    true,
							Description: "Sensitivity level of the repository. One of: extreme, sensitive, normal, not_sensitive, no_data.",
						},
						"labels": schema.ListAttribute{
							Computed:    true,
							ElementType: types.StringType,
							Description: "Label names on the repository, sorted alphabetically.",
						},
					},
				},
			},
		},
	}
}

func (d *repositoriesDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
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

func (d *repositoriesDataSource) Read(ctx context.Context, request datasource.ReadRequest, response *datasource.ReadResponse) {
	var config repositoriesDataSourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(unknownFilterDiagnostics(config)...)
	if response.Diagnostics.HasError() {
		return
	}

	allRepositories, err := repositories.All(ctx, d.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading repositories", err.Error())
		return
	}

	matched, matchedIDs := matchingRepositories(allRepositories, config)
	config.Repositories = matched
	config.IDs = matchedIDs

	response.Diagnostics.Append(response.State.Set(ctx, &config)...)
}

// matchingRepositories filters and maps in a single pass, so that ids stays
// aligned with repositories entry for entry. Both are always non-nil, so that
// no match yields an empty list rather than null.
func matchingRepositories(allRepositories []repositories.Repository, config repositoriesDataSourceModel) ([]repositoryModel, []types.Int64) {
	matched := make([]repositoryModel, 0, len(allRepositories))
	matchedIDs := make([]types.Int64, 0, len(allRepositories))

	for _, apiRepository := range allRepositories {
		if !matchesFilters(apiRepository, config) {
			continue
		}
		matched = append(matched, repositoryModelFromAPI(apiRepository))
		matchedIDs = append(matchedIDs, types.Int64Value(apiRepository.ID))
	}

	return matched, matchedIDs
}

// unknownFilterDiagnostics rejects filters that are not known at read time.
// Refusing beats ignoring: an ignored filter would widen the result to every
// repository, and that result feeds repo_ids.
func unknownFilterDiagnostics(config repositoriesDataSourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	filters := []struct {
		name      string
		isUnknown bool
	}{
		{"name", config.Name.IsUnknown()},
		{"branch", config.Branch.IsUnknown()},
		{"active", config.Active.IsUnknown()},
	}

	for _, filter := range filters {
		if !filter.isUnknown {
			continue
		}
		diagnostics.AddAttributeError(
			path.Root(filter.name),
			"Unknown repository filter",
			fmt.Sprintf("The %s filter is not known at read time, so it cannot be used to select repositories. "+
				"Set it to a value that does not depend on an attribute that is only known after apply.", filter.name),
		)
	}

	return diagnostics
}

// matchesFilters reports whether a repository satisfies every set filter.
// Unset filters never exclude anything.
func matchesFilters(apiRepository repositories.Repository, config repositoriesDataSourceModel) bool {
	if !config.Name.IsNull() && apiRepository.Name != config.Name.ValueString() {
		return false
	}
	if !config.Branch.IsNull() && apiRepository.Branch != config.Branch.ValueString() {
		return false
	}
	if !config.Active.IsNull() && apiRepository.Active != config.Active.ValueBool() {
		return false
	}

	return true
}

func repositoryModelFromAPI(apiRepository repositories.Repository) repositoryModel {
	return repositoryModel{
		ID:                    types.StringValue(strconv.FormatInt(apiRepository.ID, 10)),
		Name:                  types.StringValue(apiRepository.Name),
		GitProvider:           types.StringValue(apiRepository.Provider),
		ExternalRepoID:        types.StringValue(apiRepository.ExternalRepoID),
		ExternalRepoNumericID: types.Int64Value(apiRepository.ExternalRepoNumericID),
		Active:                types.BoolValue(apiRepository.Active),
		Branch:                types.StringValue(apiRepository.Branch),
		URL:                   types.StringValue(apiRepository.URL),
		LastScannedAt:         types.Int64Value(apiRepository.LastScannedAt),
		Connectivity:          nullIfEmpty(apiRepository.Connectivity),
		Sensitivity:           nullIfEmpty(apiRepository.Sensitivity),
		Labels:                sortedLabelNames(apiRepository.Labels),
	}
}

func nullIfEmpty(value string) types.String {
	if value == "" {
		return types.StringNull()
	}

	return types.StringValue(value)
}

// sortedLabelNames returns label names in a fixed order, so that a change in the
// order the API happens to return labels does not churn dependent resources.
func sortedLabelNames(apiLabels []repositories.Label) []types.String {
	names := make([]string, 0, len(apiLabels))
	for _, apiLabel := range apiLabels {
		names = append(names, apiLabel.Name)
	}
	slices.Sort(names)

	labelNames := make([]types.String, 0, len(names))
	for _, name := range names {
		labelNames = append(labelNames, types.StringValue(name))
	}

	return labelNames
}
