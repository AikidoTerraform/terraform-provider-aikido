package datasources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/containers"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &containersDataSource{}
	_ datasource.DataSourceWithConfigure = &containersDataSource{}
)

func NewContainersDataSource() datasource.DataSource {
	return &containersDataSource{}
}

type containersDataSource struct {
	client *client.Client
}

// containersDataSourceModel holds the optional filters plus the matched
// containers. Filters are applied in-process against the shared container list
// cache, so a config that also manages aikido_container resources pays for one
// paginated list overall rather than one per data source.
type containersDataSourceModel struct {
	Name             types.String     `tfsdk:"name"`
	RegistryProvider types.String     `tfsdk:"registry_provider"`
	RegistryID       types.Int64      `tfsdk:"registry_id"`
	RegistryName     types.String     `tfsdk:"registry_name"`
	CloudID          types.Int64      `tfsdk:"cloud_id"`
	Active           types.Bool       `tfsdk:"active"`
	Labels           types.Set        `tfsdk:"labels"`
	IDs              []types.Int64    `tfsdk:"ids"`
	Containers       []containerModel `tfsdk:"containers"`
}

type containerModel struct {
	ID                types.String   `tfsdk:"id"`
	Name              types.String   `tfsdk:"name"`
	RegistryProvider  types.String   `tfsdk:"registry_provider"`
	RegistryID        types.Int64    `tfsdk:"registry_id"`
	RegistryName      types.String   `tfsdk:"registry_name"`
	CloudID           types.Int64    `tfsdk:"cloud_id"`
	TagFilter         types.String   `tfsdk:"tag_filter"`
	Active            types.Bool     `tfsdk:"active"`
	LinkedCodeRepoID  types.Int64    `tfsdk:"linked_code_repo_id"`
	Distro            types.String   `tfsdk:"distro"`
	DistroVersion     types.String   `tfsdk:"distro_version"`
	LastScannedAt     types.Int64    `tfsdk:"last_scanned_at"`
	LastScannedTag    types.String   `tfsdk:"last_scanned_tag"`
	LastScannedDigest types.String   `tfsdk:"last_scanned_digest"`
	Connectivity      types.String   `tfsdk:"connectivity"`
	Sensitivity       types.String   `tfsdk:"sensitivity"`
	Labels            []types.String `tfsdk:"labels"`
}

func (d *containersDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_containers"
}

func (d *containersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Looks up Aikido containers. " +
			"Returns every container, active and inactive, unless filters narrow the result. " +
			"Filters combine with AND; a filter that matches nothing yields an empty list rather than an error. " +
			"Use the ids attribute to feed the numeric image_id attribute of aikido_team_resource, " +
			"and the containers attribute when Terraform expressions need to select by naming convention.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional: true,
				Description: "Only return containers whose name is exactly this. " +
					"Matching is exact, not a substring or glob. " +
					"A name can match more than one container when the same repository name exists in several registries, " +
					"so combine this with registry_name, registry_id or cloud_id to select exactly one.",
			},
			"registry_provider": schema.StringAttribute{
				Optional:    true,
				Description: "Only return containers hosted by this registry provider, for example aws, acr or docker-hub.",
			},
			"registry_id": schema.Int64Attribute{
				Optional:    true,
				Description: "Only return containers held by this registry.",
			},
			"registry_name": schema.StringAttribute{
				Optional: true,
				Description: "Only return containers whose registry name is exactly this. " +
					"For AWS the registry name is the account ID, which distinguishes identically named repositories in different accounts.",
			},
			"cloud_id": schema.Int64Attribute{
				Optional:    true,
				Description: "Only return containers discovered through this cloud.",
			},
			"active": schema.BoolAttribute{
				Optional:    true,
				Description: "Only return containers with this activation state. Omit to return both active and inactive containers.",
			},
			"labels": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Only return containers carrying every one of these labels, matched exactly. " +
					"An empty set matches every container; use Terraform expressions over the containers attribute for OR and other conditions.",
			},
			"ids": schema.SetAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Numeric IDs of the matching containers. Typed to match the image_id attribute of aikido_team_resource; use one(...) to feed a single numeric image_id.",
			},
			"containers": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Matching containers, ordered by Aikido container ID.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Aikido container ID, as a string to match the id attribute of aikido_container. For the numeric image_id attribute, use the ids attribute of this data source instead.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the container.",
						},
						"registry_provider": schema.StringAttribute{
							Computed:    true,
							Description: "Provider hosting the container.",
						},
						"registry_id": schema.Int64Attribute{
							Computed:    true,
							Description: "ID of the registry holding this container. Null when the container was discovered through a cloud.",
						},
						"registry_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the registry holding this container. For AWS this is the account ID.",
						},
						"cloud_id": schema.Int64Attribute{
							Computed:    true,
							Description: "ID of the cloud acting as the registry for this container. Null when the container came from a registry.",
						},
						"tag_filter": schema.StringAttribute{
							Computed:    true,
							Description: "Tag filter deciding which image is scanned. Null means the newest image is scanned.",
						},
						"active": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the container is activated for scanning in Aikido.",
						},
						"linked_code_repo_id": schema.Int64Attribute{
							Computed:    true,
							Description: "Aikido code repository linked to this container, or null when none is linked.",
						},
						"distro": schema.StringAttribute{
							Computed:    true,
							Description: "Linux distribution detected in the scanned image.",
						},
						"distro_version": schema.StringAttribute{
							Computed:    true,
							Description: "Version of the detected distribution.",
						},
						"last_scanned_at": schema.Int64Attribute{
							Computed:    true,
							Description: "Unix timestamp of the last completed scan.",
						},
						"last_scanned_tag": schema.StringAttribute{
							Computed:    true,
							Description: "Tag that was actually scanned, as opposed to the tag_filter that selected it.",
						},
						"last_scanned_digest": schema.StringAttribute{
							Computed:    true,
							Description: "Digest of the scanned image.",
						},
						"connectivity": schema.StringAttribute{
							Computed:    true,
							Description: "Whether the container runs on an internet-connected server. One of: connected, not_connected, unknown.",
						},
						"sensitivity": schema.StringAttribute{
							Computed:    true,
							Description: "Sensitivity level of the container. One of: extreme, sensitive, normal, not_sensitive, no_data.",
						},
						"labels": schema.ListAttribute{
							Computed:    true,
							ElementType: types.StringType,
							Description: "Label names on the container, sorted alphabetically.",
						},
					},
				},
			},
		},
	}
}

func (d *containersDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
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

func (d *containersDataSource) Read(ctx context.Context, request datasource.ReadRequest, response *datasource.ReadResponse) {
	var config containersDataSourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(unknownContainerFilterDiagnostics(config)...)
	if response.Diagnostics.HasError() {
		return
	}

	wantedLabels, labelDiagnostics := labelFilter(ctx, config.Labels)
	response.Diagnostics.Append(labelDiagnostics...)
	if response.Diagnostics.HasError() {
		return
	}

	allContainers, err := containers.All(ctx, d.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading containers", err.Error())
		return
	}

	matched, matchedIDs := matchingContainers(allContainers, config, wantedLabels)
	config.Containers = matched
	config.IDs = matchedIDs

	response.Diagnostics.Append(response.State.Set(ctx, &config)...)
}

// matchingContainers filters and maps in a single pass, so that ids stays aligned
// with containers entry for entry. Both are always non-nil, so that no match
// yields an empty list rather than null.
func matchingContainers(allContainers []containers.Container, config containersDataSourceModel, wantedLabels []string) ([]containerModel, []types.Int64) {
	matched := make([]containerModel, 0, len(allContainers))
	matchedIDs := make([]types.Int64, 0, len(allContainers))

	for _, apiContainer := range allContainers {
		if !containerMatchesFilters(apiContainer, config, wantedLabels) {
			continue
		}
		matched = append(matched, containerModelFromAPI(apiContainer))
		matchedIDs = append(matchedIDs, types.Int64Value(apiContainer.ID))
	}

	return matched, matchedIDs
}

// unknownContainerFilterDiagnostics rejects filters that are not known at read
// time. Refusing beats ignoring: an ignored filter would widen the result to every
// container, and that result feeds image_id.
func unknownContainerFilterDiagnostics(config containersDataSourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	filters := []struct {
		name      string
		isUnknown bool
	}{
		{"name", config.Name.IsUnknown()},
		{"registry_provider", config.RegistryProvider.IsUnknown()},
		{"registry_id", config.RegistryID.IsUnknown()},
		{"registry_name", config.RegistryName.IsUnknown()},
		{"cloud_id", config.CloudID.IsUnknown()},
		{"active", config.Active.IsUnknown()},
		{"labels", anyUnknown(config.Labels)},
	}

	for _, filter := range filters {
		if !filter.isUnknown {
			continue
		}
		diagnostics.AddAttributeError(
			path.Root(filter.name),
			"Unknown container filter",
			fmt.Sprintf("The %s filter is not known at read time, so it cannot be used to select containers. "+
				"Set it to a value that does not depend on an attribute that is only known after apply.", filter.name),
		)
	}

	return diagnostics
}

// containerMatchesFilters reports whether a container satisfies every set filter.
// Unset filters never exclude anything. wantedLabels comes from labelFilter, so
// the set is converted once per read rather than once per container.
func containerMatchesFilters(apiContainer containers.Container, config containersDataSourceModel, wantedLabels []string) bool {
	if !config.Name.IsNull() && apiContainer.Name != config.Name.ValueString() {
		return false
	}
	if !config.RegistryProvider.IsNull() && apiContainer.Provider != config.RegistryProvider.ValueString() {
		return false
	}
	if !config.RegistryName.IsNull() && apiContainer.RegistryName != config.RegistryName.ValueString() {
		return false
	}
	if !config.RegistryID.IsNull() && !matchesOptionalID(apiContainer.RegistryID, config.RegistryID) {
		return false
	}
	if !config.CloudID.IsNull() && !matchesOptionalID(apiContainer.CloudID, config.CloudID) {
		return false
	}
	if !config.Active.IsNull() && apiContainer.Active != config.Active.ValueBool() {
		return false
	}
	if !hasAllLabels(apiContainer.Labels, wantedLabels) {
		return false
	}

	return true
}

// matchesOptionalID reports whether a nullable API ID equals the filter. An absent
// ID never matches, so filtering on a registry does not return cloud-sourced
// containers.
func matchesOptionalID(apiID *int64, filter types.Int64) bool {
	return apiID != nil && *apiID == filter.ValueInt64()
}

func containerModelFromAPI(apiContainer containers.Container) containerModel {
	return containerModel{
		ID:                types.StringValue(strconv.FormatInt(apiContainer.ID, 10)),
		Name:              types.StringValue(apiContainer.Name),
		RegistryProvider:  types.StringValue(apiContainer.Provider),
		RegistryID:        nullableInt64Value(apiContainer.RegistryID),
		RegistryName:      nullIfEmpty(apiContainer.RegistryName),
		CloudID:           nullableInt64Value(apiContainer.CloudID),
		TagFilter:         nullIfEmpty(apiContainer.TagFilter),
		Active:            types.BoolValue(apiContainer.Active),
		LinkedCodeRepoID:  nullableInt64Value(apiContainer.LinkedCodeRepoID),
		Distro:            nullIfEmpty(apiContainer.Distro),
		DistroVersion:     nullIfEmpty(apiContainer.DistroVersion),
		LastScannedAt:     types.Int64Value(apiContainer.LastScannedAt),
		LastScannedTag:    nullIfEmpty(apiContainer.LastScannedTag),
		LastScannedDigest: nullIfEmpty(apiContainer.LastScannedDigest),
		Connectivity:      nullIfEmpty(apiContainer.Connectivity),
		Sensitivity:       nullIfEmpty(apiContainer.Sensitivity),
		Labels:            sortedLabelNames(apiContainer.Labels),
	}
}

// nullableInt64Value keeps an absent ID null rather than 0, so that a missing
// registry is not mistaken for registry 0.
func nullableInt64Value(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}

	return types.Int64Value(*value)
}
