package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                = &AzureCloudResource{}
	_ resource.ResourceWithImportState = &AzureCloudResource{}
	_ resource.ResourceWithConfigure   = &AzureCloudResource{}
)

func NewAzureCloudResource() resource.Resource {
	return &AzureCloudResource{}
}

type AzureCloudResource struct {
	client *client.Client
}

type AzureCloudModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	ApplicationID    types.String `tfsdk:"application_id"`
	DirectoryID      types.String `tfsdk:"directory_id"`
	SubscriptionID   types.String `tfsdk:"subscription_id"`
	KeyValue         types.String `tfsdk:"key_value"`
	Environment      types.String `tfsdk:"environment"`
	AzureEnvironment types.String `tfsdk:"azure_environment"`
	Organization     types.Object `tfsdk:"organization"`
	ExternalID       types.String `tfsdk:"external_id"`
	LastScannedAt    types.Int64  `tfsdk:"last_scanned_at"`
}

type AzureOrganizationModel struct {
	IsManagementAccount        types.Bool   `tfsdk:"is_management_account"`
	OrganizationID             types.String `tfsdk:"organization_id"`
	ManagementGroupIDs         types.List   `tfsdk:"management_group_ids"`
	ExcludedManagementGroupIDs types.List   `tfsdk:"excluded_management_group_ids"`
	ExcludedSubscriptionIDs    types.List   `tfsdk:"excluded_subscription_ids"`
	ACRScanEnabled             types.Bool   `tfsdk:"acr_scan_enabled"`
	VMScanEnabled              types.Bool   `tfsdk:"vm_scan_enabled"`
}

func (r *AzureCloudResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_azure_cloud"
}

func (r *AzureCloudResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Connects an Azure subscription or management account to Aikido using an existing registered application. Use aikido_team_resource to link the cloud to a team.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido cloud ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name for this cloud connection.",
			},
			"application_id": schema.StringAttribute{
				Required:    true,
				Description: "Application (client) ID of the registered application. Changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"directory_id": schema.StringAttribute{
				Required:    true,
				Description: "Azure directory (tenant) ID. Changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"subscription_id": schema.StringAttribute{
				Required:    true,
				Description: "Subscription ID. When organization is set, this is the management subscription ID. Changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key_value": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Client secret for the registered application. Changes rotate the credentials. The API does not return this value; it is stored in Terraform state.",
			},
			"environment": schema.StringAttribute{
				Required:    true,
				Description: "Environment purpose used for severity weighting: production, staging, development, or mixed.",
				Validators: []validator.String{
					stringvalidator.OneOf(clouds.SupportedEnvironments...),
				},
			},
			"azure_environment": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public"),
				Description: "Azure cloud environment: public, gov, or china. Defaults to public. Changing it forces replacement.",
				Validators: []validator.String{
					stringvalidator.OneOf("public", "gov", "china"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"organization": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Connects a management account and syncs subscriptions in the selected management groups. Adding or removing this block forces replacement. Sync scopes and exclusions are updatable; scan flags are create-only.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplaceIf(
						func(_ context.Context, request planmodifier.ObjectRequest, response *objectplanmodifier.RequiresReplaceIfFuncResponse) {
							response.RequiresReplace = request.StateValue.IsNull() != request.PlanValue.IsNull()
						},
						"organization presence",
						"Adding or removing organization forces replacement.",
					),
				},
				Attributes: map[string]schema.Attribute{
					"is_management_account": schema.BoolAttribute{
						Computed:    true,
						Description: "True for a connected Azure management account.",
					},
					"organization_id": schema.StringAttribute{
						Computed:    true,
						Description: "Azure directory ID of the connected organization.",
					},
					"management_group_ids": schema.ListAttribute{
						Optional:    true,
						Computed:    true,
						ElementType: types.StringType,
						Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
						Description: "Management group IDs to sync. An empty list means tenant root scope.",
					},
					"excluded_management_group_ids": schema.ListAttribute{
						Optional:    true,
						Computed:    true,
						ElementType: types.StringType,
						Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
						Description: "Management group IDs excluded from sync. Defaults to an empty list.",
					},
					"excluded_subscription_ids": schema.ListAttribute{
						Optional:    true,
						Computed:    true,
						ElementType: types.StringType,
						Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
						Description: "Subscription IDs excluded from sync. Defaults to an empty list.",
					},
					"acr_scan_enabled": schema.BoolAttribute{
						Optional:    true,
						Computed:    true,
						Default:     booldefault.StaticBool(false),
						Description: "Enable organization-wide Azure Container Registry scanning. Defaults to false. Changing it forces replacement.",
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.RequiresReplace(),
						},
					},
					"vm_scan_enabled": schema.BoolAttribute{
						Optional:    true,
						Computed:    true,
						Default:     booldefault.StaticBool(false),
						Description: "Enable organization-wide VM scanning. Defaults to false. Changing it forces replacement.",
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.RequiresReplace(),
						},
					},
				},
			},
			"external_id": schema.StringAttribute{
				Computed:    true,
				Description: "Azure subscription ID returned by Aikido.",
			},
			"last_scanned_at": schema.Int64Attribute{
				Computed:    true,
				Description: "Unix timestamp of the last scan, or -1 when no scan has completed.",
			},
		},
	}
}

func (r *AzureCloudResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
	if request.ProviderData == nil {
		return
	}

	apiClient, ok := request.ProviderData.(*client.Client)
	if !ok {
		response.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", request.ProviderData))
		return
	}

	r.client = apiClient
}

func (r *AzureCloudResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned AzureCloudModel

	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	var id int64
	var err error

	if planned.Organization.IsNull() || planned.Organization.IsUnknown() {
		id, err = clouds.CreateAzure(ctx, r.client, createAzureInput(planned))
	} else {
		input, diagnostics := createAzureOrganizationInput(ctx, planned)

		response.Diagnostics.Append(diagnostics...)
		if response.Diagnostics.HasError() {
			return
		}

		id, err = clouds.CreateAzureOrganization(ctx, r.client, input)
	}

	if err != nil {
		response.Diagnostics.AddError("Error creating Azure cloud", err.Error())
		return
	}

	state, diagnostics := r.readBack(ctx, id, planned)
	response.Diagnostics.Append(diagnostics...)
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *AzureCloudResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var prior AzureCloudModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	id, err := clouds.ParseCloudID(prior.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading Azure cloud", err.Error())
		return
	}

	cloud, err := clouds.ByID(ctx, r.client, id)
	if err != nil {
		if client.NotInList(err) {
			response.State.RemoveResource(ctx)
			return
		}

		response.Diagnostics.AddError("Error reading Azure cloud", err.Error())
		return
	}

	state, diagnostics := AzureCloudModelFromAPI(ctx, cloud)
	response.Diagnostics.Append(diagnostics...)

	if response.Diagnostics.HasError() {
		return
	}

	preserveAzureWriteOnlyState(&state, prior)
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *AzureCloudResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned, prior AzureCloudModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	id, err := clouds.ParseCloudID(planned.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error updating Azure cloud", err.Error())
		return
	}

	if err := clouds.Update(ctx, r.client, id, clouds.UpdateCloudInput{
		Name:        planned.Name.ValueString(),
		Environment: planned.Environment.ValueString(),
	}); err != nil {
		response.Diagnostics.AddError("Error updating Azure cloud", err.Error())
		return
	}

	if !planned.KeyValue.Equal(prior.KeyValue) {
		if err := clouds.UpdateAzureCloudCredentials(ctx, r.client, id, clouds.UpdateAzureCloudCredentialsInput{
			ClientSecret: planned.KeyValue.ValueString(),
		}); err != nil {
			response.Diagnostics.AddError("Error updating Azure cloud credentials", err.Error())
			return
		}
	}

	if !planned.Organization.IsNull() {
		input, diagnostics := updateAzureOrganizationInput(ctx, planned)
		response.Diagnostics.Append(diagnostics...)

		if response.Diagnostics.HasError() {
			return
		}

		if err := clouds.UpdateOrganization(ctx, r.client, id, input); err != nil {
			response.Diagnostics.AddError("Error updating Azure cloud organization", err.Error())
			return
		}
	}

	state, diagnostics := r.readBack(ctx, id, planned)
	response.Diagnostics.Append(diagnostics...)

	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *AzureCloudResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var prior AzureCloudModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	id, err := clouds.ParseCloudID(prior.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error deleting Azure cloud", err.Error())
		return
	}

	if err := clouds.Delete(ctx, r.client, id); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deleting Azure cloud", err.Error())
	}
}

func (r *AzureCloudResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *AzureCloudResource) readBack(ctx context.Context, id int64, planned AzureCloudModel) (AzureCloudModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	fallback := planned
	fallback.ID = types.StringValue(strconv.FormatInt(id, 10))
	fallback.ExternalID = types.StringNull()
	fallback.LastScannedAt = types.Int64Null()

	cloud, err := clouds.ByID(ctx, r.client, id)
	if err != nil {
		diagnostics.AddError("Error reading Azure cloud back", err.Error())
		return fallback, diagnostics
	}

	state, diagnostics := AzureCloudModelFromAPI(ctx, cloud)
	if diagnostics.HasError() {
		return fallback, diagnostics
	}

	preserveAzureWriteOnlyState(&state, planned)
	return state, diagnostics
}

func createAzureInput(planned AzureCloudModel) clouds.CreateAzureInput {
	return clouds.CreateAzureInput{
		Name:             planned.Name.ValueString(),
		ApplicationId:    planned.ApplicationID.ValueString(),
		DirectoryId:      planned.DirectoryID.ValueString(),
		SubscriptionId:   planned.SubscriptionID.ValueString(),
		KeyValue:         planned.KeyValue.ValueString(),
		Environment:      planned.Environment.ValueString(),
		AzureEnvironment: planned.AzureEnvironment.ValueString(),
	}
}

func createAzureOrganizationInput(ctx context.Context, planned AzureCloudModel) (clouds.CreateAzureOrganizationInput, diag.Diagnostics) {
	var org AzureOrganizationModel

	diagnostics := planned.Organization.As(ctx, &org, basetypes.ObjectAsOptions{})
	if diagnostics.HasError() {
		return clouds.CreateAzureOrganizationInput{}, diagnostics
	}

	sync, listDiagnostics := updateAzureOrganizationInput(ctx, planned)
	diagnostics.Append(listDiagnostics...)

	if diagnostics.HasError() {
		return clouds.CreateAzureOrganizationInput{}, diagnostics
	}

	return clouds.CreateAzureOrganizationInput{
		Name:                       planned.Name.ValueString(),
		ApplicationId:              planned.ApplicationID.ValueString(),
		DirectoryId:                planned.DirectoryID.ValueString(),
		ManagementSubscriptionId:   planned.SubscriptionID.ValueString(),
		KeyValue:                   planned.KeyValue.ValueString(),
		Environment:                planned.Environment.ValueString(),
		AzureEnvironment:           planned.AzureEnvironment.ValueString(),
		ManagementGroupIDs:         sync.SyncedUnitIDs,
		ExcludedManagementGroupIDs: sync.ExcludedFolders,
		ExcludedSubscriptionIDs:    sync.ExcludedAccounts,
		ACRScanEnabled:             org.ACRScanEnabled.ValueBool(),
		VMScanEnabled:              org.VMScanEnabled.ValueBool(),
	}, diagnostics
}

func updateAzureOrganizationInput(ctx context.Context, planned AzureCloudModel) (clouds.UpdateCloudOrganizationInput, diag.Diagnostics) {
	var org AzureOrganizationModel

	diagnostics := planned.Organization.As(ctx, &org, basetypes.ObjectAsOptions{})
	if diagnostics.HasError() {
		return clouds.UpdateCloudOrganizationInput{}, diagnostics
	}

	groups, d := clouds.StringListValues(ctx, org.ManagementGroupIDs)
	diagnostics.Append(d...)

	excludedGroups, d := clouds.StringListValues(ctx, org.ExcludedManagementGroupIDs)
	diagnostics.Append(d...)

	excludedSubscriptions, d := clouds.StringListValues(ctx, org.ExcludedSubscriptionIDs)
	diagnostics.Append(d...)

	return clouds.UpdateCloudOrganizationInput{
		SyncedUnitIDs:    strings.Join(groups, ","),
		ExcludedFolders:  strings.Join(excludedGroups, ","),
		ExcludedAccounts: strings.Join(excludedSubscriptions, ","),
	}, diagnostics
}

func AzureCloudModelFromAPI(ctx context.Context, cloud clouds.Cloud) (AzureCloudModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	if cloud.Provider != clouds.ProviderAzure || cloud.Azure == nil {
		diagnostics.AddError("Cloud is not an Azure cloud", fmt.Sprintf("Cloud %d has provider %q; aikido_azure_cloud requires Azure details.", cloud.ID, cloud.Provider))
		return AzureCloudModel{}, diagnostics
	}

	state := AzureCloudModel{
		ID:               types.StringValue(strconv.FormatInt(cloud.ID, 10)),
		Name:             types.StringValue(cloud.Name),
		ApplicationID:    types.StringValue(cloud.Azure.ApplicationId),
		DirectoryID:      types.StringValue(cloud.Azure.DirectoryId),
		SubscriptionID:   types.StringValue(cloud.ExternalID),
		KeyValue:         types.StringNull(),
		Environment:      types.StringValue(cloud.Environment),
		AzureEnvironment: types.StringValue(cloud.Azure.AzureEnvironment),
		ExternalID:       types.StringValue(cloud.ExternalID),
		LastScannedAt:    types.Int64Value(cloud.LastScannedAt),
		Organization:     types.ObjectNull(AzureOrganizationAttributeTypes()),
	}

	if cloud.Azure.IsManagementAccount {
		if cloud.Azure.Organization == nil {
			diagnostics.AddError("Missing Azure organization details", fmt.Sprintf("Cloud %d is a management account but the API returned no organization.", cloud.ID))
			return state, diagnostics
		}

		org := cloud.Azure.Organization
		groupsToSync := clouds.AzureManagementGroupIDs(org.SyncedUnitIDs, cloud.Azure.DirectoryId)

		syncedGroups, d := types.ListValueFrom(ctx, types.StringType, groupsToSync)
		diagnostics.Append(d...)

		excludedGroups, d := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.ExcludedFolders))
		diagnostics.Append(d...)

		excludedSubscriptions, d := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.ExcludedAccounts))
		diagnostics.Append(d...)

		if diagnostics.HasError() {
			return state, diagnostics
		}

		state.Organization, d = types.ObjectValueFrom(ctx, AzureOrganizationAttributeTypes(), AzureOrganizationModel{
			IsManagementAccount:        types.BoolValue(true),
			OrganizationID:             types.StringValue(org.OrganizationID),
			ManagementGroupIDs:         syncedGroups,
			ExcludedManagementGroupIDs: excludedGroups,
			ExcludedSubscriptionIDs:    excludedSubscriptions,
			ACRScanEnabled:             types.BoolValue(org.ContainerScanEnabled),
			VMScanEnabled:              types.BoolValue(org.VMScanEnabled),
		})

		diagnostics.Append(d...)
	}

	return state, diagnostics
}

func preserveAzureWriteOnlyState(state *AzureCloudModel, prior AzureCloudModel) {
	state.KeyValue = prior.KeyValue
}

func AzureOrganizationAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"is_management_account":         types.BoolType,
		"organization_id":               types.StringType,
		"management_group_ids":          types.ListType{ElemType: types.StringType},
		"excluded_management_group_ids": types.ListType{ElemType: types.StringType},
		"excluded_subscription_ids":     types.ListType{ElemType: types.StringType},
		"acr_scan_enabled":              types.BoolType,
		"vm_scan_enabled":               types.BoolType,
	}
}
