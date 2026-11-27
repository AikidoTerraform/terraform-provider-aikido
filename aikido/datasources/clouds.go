package datasources

import (
	"context"
	"fmt"
	"strconv"

	cloudhelpers "github.com/AikidoTerraform/terraform-provider-aikido/aikido/datasources/clouds"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &cloudsDataSource{}
	_ datasource.DataSourceWithConfigure = &cloudsDataSource{}
)

func NewCloudsDataSource() datasource.DataSource {
	return &cloudsDataSource{}
}

type cloudsDataSource struct {
	client *client.Client
}

type cloudsDataSourceModel struct {
	Name                types.String  `tfsdk:"name"`
	CloudProvider       types.String  `tfsdk:"cloud_provider"`
	Environment         types.String  `tfsdk:"environment"`
	ExternalID          types.String  `tfsdk:"external_id"`
	IsManagementAccount types.Bool    `tfsdk:"is_management_account"`
	IDs                 []types.Int64 `tfsdk:"ids"`
	Clouds              []cloudModel  `tfsdk:"clouds"`
}

type cloudModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	CloudProvider types.String `tfsdk:"cloud_provider"`
	Environment   types.String `tfsdk:"environment"`
	ExternalID    types.String `tfsdk:"external_id"`
	LastScannedAt types.Int64  `tfsdk:"last_scanned_at"`
	AWS           types.Object `tfsdk:"aws"`
	Azure         types.Object `tfsdk:"azure"`
}

func (d *cloudsDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_clouds"
}

func (d *cloudsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Looks up connected Aikido AWS and Azure clouds. " +
			"Filters combine with AND; a filter that matches nothing yields an empty list rather than an error.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Only return clouds whose name is exactly this. Matching is exact, not a substring or glob.",
			},
			"cloud_provider": schema.StringAttribute{
				Optional:    true,
				Description: "Only return clouds for this provider: aws or azure.",
				Validators: []validator.String{
					stringvalidator.OneOf(clouds.SupportedProviders...),
				},
			},
			"environment": schema.StringAttribute{
				Optional:    true,
				Description: "Only return clouds with this environment purpose. One of: production, staging, development, mixed.",
				Validators: []validator.String{
					stringvalidator.OneOf(clouds.SupportedEnvironments...),
				},
			},
			"external_id": schema.StringAttribute{
				Optional:    true,
				Description: "Only return clouds whose provider-specific external ID is exactly this. For AWS this is the account ID; for Azure it is the subscription ID.",
			},
			"is_management_account": schema.BoolAttribute{
				Optional:    true,
				Description: "Only return organization management accounts (true) or non-management clouds (false).",
			},
			"ids": schema.SetAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Numeric IDs of the matching clouds. Typed to match the cloud_id attribute of aikido_team_resource; use one(...) to feed a single numeric cloud_id.",
			},
			"clouds": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Matching clouds, ordered by Aikido cloud ID.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Aikido cloud ID.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Display name of the cloud connection.",
						},
						"cloud_provider": schema.StringAttribute{
							Computed:    true,
							Description: "Cloud provider identifier.",
						},
						"environment": schema.StringAttribute{
							Computed:    true,
							Description: "Environment purpose. One of: production, staging, development, mixed.",
						},
						"external_id": schema.StringAttribute{
							Computed:    true,
							Description: "Provider-specific external ID. For AWS this is the account ID; for Azure it is the subscription ID.",
						},
						"last_scanned_at": schema.Int64Attribute{
							Computed:    true,
							Description: "Unix timestamp of the last cloud scan, or -1 when no scan has completed yet.",
						},
						"azure": cloudhelpers.AzureSchema(),
						"aws":   cloudhelpers.AWSSchema(),
					},
				},
			},
		},
	}
}

func (d *cloudsDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
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

func (d *cloudsDataSource) Read(ctx context.Context, request datasource.ReadRequest, response *datasource.ReadResponse) {
	var config cloudsDataSourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(unknownCloudFilterDiagnostics(config)...)
	if response.Diagnostics.HasError() {
		return
	}

	allClouds, err := clouds.GetAllCloudsBySupportedProvider(ctx, d.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading clouds", err.Error())
		return
	}

	matched, matchedIDs, diagnostics := matchingClouds(ctx, allClouds, config)
	response.Diagnostics.Append(diagnostics...)

	if response.Diagnostics.HasError() {
		return
	}

	config.Clouds = matched
	config.IDs = matchedIDs

	response.Diagnostics.Append(response.State.Set(ctx, &config)...)
}

func matchingClouds(ctx context.Context, allClouds []clouds.Cloud, config cloudsDataSourceModel) ([]cloudModel, []types.Int64, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	matched := make([]cloudModel, 0, len(allClouds))
	matchedIDs := make([]types.Int64, 0, len(allClouds))

	for _, apiCloud := range allClouds {
		if !cloudMatchesFilters(apiCloud, config) {
			continue
		}

		model, modelDiagnostics := cloudModelFromAPI(ctx, apiCloud)
		diagnostics.Append(modelDiagnostics...)

		if diagnostics.HasError() {
			return nil, nil, diagnostics
		}

		matched = append(matched, model)
		matchedIDs = append(matchedIDs, types.Int64Value(apiCloud.ID))
	}

	return matched, matchedIDs, diagnostics
}

func cloudMatchesFilters(cloud clouds.Cloud, config cloudsDataSourceModel) bool {
	if !config.Name.IsNull() && cloud.Name != config.Name.ValueString() {
		return false
	}
	if !config.CloudProvider.IsNull() && cloud.Provider != config.CloudProvider.ValueString() {
		return false
	}
	if !config.Environment.IsNull() && cloud.Environment != config.Environment.ValueString() {
		return false
	}
	if !config.ExternalID.IsNull() && cloud.ExternalID != config.ExternalID.ValueString() {
		return false
	}

	if !config.IsManagementAccount.IsNull() {
		var isManagement bool

		switch cloud.Provider {
		case clouds.ProviderAWS:
			isManagement = cloud.AWS != nil && cloud.AWS.IsManagementAccount
		case clouds.ProviderAzure:
			isManagement = cloud.Azure != nil && cloud.Azure.IsManagementAccount
		}

		if isManagement != config.IsManagementAccount.ValueBool() {
			return false
		}
	}

	return true
}

func cloudModelFromAPI(ctx context.Context, cloud clouds.Cloud) (cloudModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	model := cloudModel{
		ID:            types.StringValue(strconv.FormatInt(cloud.ID, 10)),
		Name:          types.StringValue(cloud.Name),
		CloudProvider: types.StringValue(cloud.Provider),
		Environment:   types.StringValue(cloud.Environment),
		ExternalID:    types.StringValue(cloud.ExternalID),
		LastScannedAt: types.Int64Value(cloud.LastScannedAt),
	}

	awsObject, awsDiagnostics := cloudhelpers.AWSObjectValue(ctx, cloud.AWS)
	diagnostics.Append(awsDiagnostics...)
	if diagnostics.HasError() {
		return cloudModel{}, diagnostics
	}
	model.AWS = awsObject

	azureObject, azureDiagnostics := cloudhelpers.AzureObjectValue(ctx, cloud.ID, cloud.Azure)
	diagnostics.Append(azureDiagnostics...)
	if diagnostics.HasError() {
		return cloudModel{}, diagnostics
	}
	model.Azure = azureObject

	return model, diagnostics
}

func unknownCloudFilterDiagnostics(config cloudsDataSourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	for _, check := range []struct {
		name  string
		value interface{ IsUnknown() bool }
	}{
		{"name", config.Name},
		{"cloud_provider", config.CloudProvider},
		{"environment", config.Environment},
		{"external_id", config.ExternalID},
		{"is_management_account", config.IsManagementAccount},
	} {
		if check.value.IsUnknown() {
			diagnostics.AddAttributeError(
				path.Root(check.name),
				"Unknown cloud filter",
				fmt.Sprintf("The %s filter is unknown. Set a concrete value or omit the attribute.", check.name),
			)
		}
	}

	return diagnostics
}
