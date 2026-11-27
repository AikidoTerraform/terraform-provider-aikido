package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                = &awsCloudResource{}
	_ resource.ResourceWithImportState = &awsCloudResource{}
	_ resource.ResourceWithConfigure   = &awsCloudResource{}
)

func NewAWSCloudResource() resource.Resource {
	return &awsCloudResource{}
}

type awsCloudResource struct {
	client *client.Client
}

type awsCloudModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	RoleARN       types.String `tfsdk:"role_arn"`
	Environment   types.String `tfsdk:"environment"`
	Organization  types.Object `tfsdk:"organization"`
	ExternalID    types.String `tfsdk:"external_id"`
	LastScannedAt types.Int64  `tfsdk:"last_scanned_at"`
}

type awsOrganizationModel struct {
	IsManagementAccount  types.Bool   `tfsdk:"is_management_account"`
	OrganizationID       types.String `tfsdk:"organization_id"`
	SyncedUnitIDs        types.List   `tfsdk:"synced_unit_ids"`
	ExcludedAccounts     types.List   `tfsdk:"excluded_accounts"`
	ContainerScanEnabled types.Bool   `tfsdk:"container_scan_enabled"`
	VMScanEnabled        types.Bool   `tfsdk:"vm_scan_enabled"`
}

func (r *awsCloudResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_aws_cloud"
}

func (r *awsCloudResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Connects an AWS cloud environment to Aikido, either as a single account or as an AWS Organization management account. " +
			"Create the IAM role in AWS first (see the Aikido AWS cloud documentation); this resource does not generate CloudFormation or console links. " +
			"Use aikido_team_resource to link the resulting cloud to a team.",
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
			"role_arn": schema.StringAttribute{
				Required: true,
				Description: "IAM role ARN that Aikido assumes to scan this AWS account. " +
					"Create the role before applying (see Aikido AWS cloud docs). " +
					"Updatable only for single-account connections; changing it on an organization-linked cloud forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						roleARNRequiresReplaceWhenOrganization,
						"organization role ARN",
						"Changing the role ARN on an organization-linked cloud forces a new cloud connection.",
					),
				},
			},
			"environment": schema.StringAttribute{
				Required:    true,
				Description: "Environment purpose used for severity weighting. One of production, staging, development, mixed.",
				Validators: []validator.String{
					stringvalidator.OneOf(clouds.SupportedEnvironments...),
				},
			},
			"organization": schema.SingleNestedAttribute{
				Optional: true,
				Description: "When set, connects this AWS account as an organization management account and syncs member accounts. " +
					"Adding or removing this block forces replacement. " +
					"synced_unit_ids and excluded_accounts are updatable; organization_id and the ECR/EBS scan flags are create-only.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplaceIf(
						func(_ context.Context, request planmodifier.ObjectRequest, response *objectplanmodifier.RequiresReplaceIfFuncResponse) {
							response.RequiresReplace = request.StateValue.IsNull() != request.PlanValue.IsNull()
						},
						"organization presence",
						"Adding or removing the organization block forces a new cloud connection.",
					),
				},
				Attributes: map[string]schema.Attribute{
					"is_management_account": schema.BoolAttribute{
						Computed:    true,
						Description: "True when this cloud is the management account of a connected AWS Organization.",
					},
					"organization_id": schema.StringAttribute{
						Required:    true,
						Description: "AWS Organizations ID, for example o-1234567890. Changing it forces replacement.",
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
						},
					},
					"synced_unit_ids": schema.ListAttribute{
						Required:    true,
						ElementType: types.StringType,
						Description: "Root ID (r-...) or Organization Unit IDs (ou-...) to sync. Provide the root ID to connect the entire organization.",
						Validators: []validator.List{
							listvalidator.SizeAtLeast(1),
						},
					},
					"excluded_accounts": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Optional AWS account IDs to exclude from organization sync. Defaults to an empty list.",
						Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
					},
					"container_scan_enabled": schema.BoolAttribute{
						Optional:    true,
						Description: "When true, automatically configure ECR scanning across the organization. Defaults to false. Create-only; changing it forces replacement.",
						Default:     booldefault.StaticBool(false),
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.RequiresReplace(),
						},
					},
					"vm_scan_enabled": schema.BoolAttribute{
						Optional:    true,
						Description: "When true, automatically configure EBS/EC2 scanning across the organization. Defaults to false. Create-only; changing it forces replacement.",
						Default:     booldefault.StaticBool(false),
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.RequiresReplace(),
						},
					},
				},
			},
			"external_id": schema.StringAttribute{
				Computed:    true,
				Description: "AWS account ID extracted from the role ARN.",
			},
			"last_scanned_at": schema.Int64Attribute{
				Computed:    true,
				Description: "Unix timestamp of the last cloud scan, or -1 when no scan has completed yet.",
			},
		},
	}
}

func (r *awsCloudResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *awsCloudResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned awsCloudModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	var id int64
	var err error

	if planned.Organization.IsNull() || planned.Organization.IsUnknown() {
		id, err = r.createSingleAccount(ctx, planned, response)
	} else {
		id, err = r.createOrganization(ctx, planned, response)
	}

	if response.Diagnostics.HasError() {
		return
	}
	if err != nil {
		response.Diagnostics.AddError("Error creating AWS cloud", err.Error())
		return
	}

	state, diagnostics := r.readBack(ctx, id, planned)
	response.Diagnostics.Append(diagnostics...)

	if state.ID.IsNull() {
		state = identityOnlyAWSCloud(id, planned)
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *awsCloudResource) createSingleAccount(ctx context.Context, planned awsCloudModel, _ *resource.CreateResponse) (int64, error) {
	return clouds.CreateAWS(ctx, r.client, createAWSInput(planned))
}

func (r *awsCloudResource) createOrganization(ctx context.Context, planned awsCloudModel, response *resource.CreateResponse) (int64, error) {
	input, diagnostics := createAWSOrganizationInput(ctx, planned)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return 0, nil
	}

	return clouds.CreateAWSOrganization(ctx, r.client, input)
}

func (r *awsCloudResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState awsCloudModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseCloudID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading AWS cloud", err.Error())
		return
	}

	cloud, err := clouds.ByID(ctx, r.client, id)
	if err != nil {
		if client.NotInList(err) {
			response.State.RemoveResource(ctx)
			return
		}

		response.Diagnostics.AddError("Error reading AWS cloud", err.Error())
		return
	}

	if cloud.Provider != clouds.ProviderAWS {
		response.Diagnostics.AddError(
			"Cloud is not an AWS cloud",
			fmt.Sprintf("Cloud %d has provider %q; aikido_aws_cloud only manages AWS connections.", id, cloud.Provider),
		)

		return
	}

	state, diagnostics := awsCloudModelFromAPI(ctx, cloud)
	response.Diagnostics.Append(diagnostics...)

	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *awsCloudResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned awsCloudModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	var priorState awsCloudModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseCloudID(planned.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error updating AWS cloud", err.Error())
		return
	}

	if err := clouds.Update(ctx, r.client, id, clouds.UpdateCloudInput{
		Name:        planned.Name.ValueString(),
		Environment: planned.Environment.ValueString(),
	}); err != nil {
		response.Diagnostics.AddError("Error updating AWS cloud", err.Error())
		return
	}

	if planned.Organization.IsNull() {
		if priorState.RoleARN.ValueString() != planned.RoleARN.ValueString() {
			if err := clouds.UpdateAwsCloudCredentials(ctx, r.client, id, clouds.UpdateAwsCloudCredentialsInput{
				RoleARN: planned.RoleARN.ValueString(),
			}); err != nil {
				response.Diagnostics.AddError("Error updating AWS cloud credentials", err.Error())
				return
			}
		}
	} else {
		orgInput, diagnostics := updateAWSOrganizationInput(ctx, planned)
		response.Diagnostics.Append(diagnostics...)
		if response.Diagnostics.HasError() {
			return
		}

		if err := clouds.UpdateOrganization(ctx, r.client, id, orgInput); err != nil {
			response.Diagnostics.AddError("Error updating AWS cloud organization", err.Error())
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

func (r *awsCloudResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState awsCloudModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseCloudID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error deleting AWS cloud", err.Error())
		return
	}

	if err := clouds.Delete(ctx, r.client, id); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deleting AWS cloud", err.Error())
	}
}

func (r *awsCloudResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *awsCloudResource) readBack(ctx context.Context, id int64, planned awsCloudModel) (awsCloudModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	cloud, err := clouds.ByID(ctx, r.client, id)
	if err != nil {
		diagnostics.AddError("Error reading AWS cloud back", err.Error())
		return identityOnlyAWSCloud(id, planned), diagnostics
	}

	state, modelDiagnostics := awsCloudModelFromAPI(ctx, cloud)
	diagnostics.Append(modelDiagnostics...)

	return state, diagnostics
}

func createAWSInput(planned awsCloudModel) clouds.CreateAWSInput {
	return clouds.CreateAWSInput{
		Name:        planned.Name.ValueString(),
		RoleARN:     planned.RoleARN.ValueString(),
		Environment: planned.Environment.ValueString(),
	}
}

func createAWSOrganizationInput(ctx context.Context, planned awsCloudModel) (clouds.CreateAWSOrganizationInput, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	var org awsOrganizationModel
	diagnostics.Append(planned.Organization.As(ctx, &org, basetypes.ObjectAsOptions{})...)
	if diagnostics.HasError() {
		return clouds.CreateAWSOrganizationInput{}, diagnostics
	}

	syncedUnitIDs, listDiagnostics := stringListValues(ctx, org.SyncedUnitIDs)
	diagnostics.Append(listDiagnostics...)
	excludedAccounts, listDiagnostics := stringListValues(ctx, org.ExcludedAccounts)
	diagnostics.Append(listDiagnostics...)
	if diagnostics.HasError() {
		return clouds.CreateAWSOrganizationInput{}, diagnostics
	}

	return clouds.CreateAWSOrganizationInput{
		Name:                 planned.Name.ValueString(),
		RoleARN:              planned.RoleARN.ValueString(),
		Environment:          planned.Environment.ValueString(),
		OrganizationID:       org.OrganizationID.ValueString(),
		SyncedUnitIDs:        strings.Join(syncedUnitIDs, ","),
		ExcludedAccounts:     strings.Join(excludedAccounts, ","),
		ContainerScanEnabled: !org.ContainerScanEnabled.IsNull() && org.ContainerScanEnabled.ValueBool(),
		VMScanEnabled:        !org.VMScanEnabled.IsNull() && org.VMScanEnabled.ValueBool(),
	}, diagnostics
}

func updateAWSOrganizationInput(ctx context.Context, planned awsCloudModel) (clouds.UpdateCloudOrganizationInput, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	var plannedOrg awsOrganizationModel
	diagnostics.Append(planned.Organization.As(ctx, &plannedOrg, basetypes.ObjectAsOptions{})...)
	if diagnostics.HasError() {
		return clouds.UpdateCloudOrganizationInput{}, diagnostics
	}

	syncedUnitIDs, listDiagnostics := stringListValues(ctx, plannedOrg.SyncedUnitIDs)
	diagnostics.Append(listDiagnostics...)

	excludedAccounts, listDiagnostics := stringListValues(ctx, plannedOrg.ExcludedAccounts)
	diagnostics.Append(listDiagnostics...)

	if diagnostics.HasError() {
		return clouds.UpdateCloudOrganizationInput{}, diagnostics
	}

	return clouds.UpdateCloudOrganizationInput{
		SyncedUnitIDs:    strings.Join(syncedUnitIDs, ","),
		ExcludedAccounts: strings.Join(excludedAccounts, ","),
	}, diagnostics
}

func awsCloudModelFromAPI(ctx context.Context, cloud clouds.Cloud) (awsCloudModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	state := awsCloudModel{
		ID:            types.StringValue(strconv.FormatInt(cloud.ID, 10)),
		Name:          types.StringValue(cloud.Name),
		RoleARN:       types.StringNull(),
		Environment:   types.StringValue(cloud.Environment),
		ExternalID:    types.StringValue(cloud.ExternalID),
		LastScannedAt: types.Int64Value(cloud.LastScannedAt),
		Organization:  types.ObjectNull(awsOrganizationAttributeTypes()),
	}

	if cloud.AWS != nil {
		state.RoleARN = types.StringValue(cloud.AWS.RoleARN)

		if cloud.AWS.IsManagementAccount && cloud.AWS.Organization != nil {
			orgObject, orgDiagnostics := organizationObjectValue(ctx, *cloud.AWS.Organization, cloud.AWS.IsManagementAccount)
			diagnostics.Append(orgDiagnostics...)
			state.Organization = orgObject
		}
	}

	return state, diagnostics
}

func organizationObjectValue(ctx context.Context, org clouds.CloudOrganization, isManagementAccount bool) (types.Object, diag.Diagnostics) {
	synced, diagnostics := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.SyncedUnitIDs))
	excluded, excludedDiagnostics := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.ExcludedAccounts))
	diagnostics.Append(excludedDiagnostics...)
	if diagnostics.HasError() {
		return types.ObjectNull(awsOrganizationAttributeTypes()), diagnostics
	}

	return types.ObjectValueFrom(ctx, awsOrganizationAttributeTypes(), awsOrganizationModel{
		IsManagementAccount:  types.BoolValue(isManagementAccount),
		OrganizationID:       types.StringValue(org.OrganizationID),
		SyncedUnitIDs:        synced,
		ExcludedAccounts:     excluded,
		ContainerScanEnabled: types.BoolValue(org.ContainerScanEnabled),
		VMScanEnabled:        types.BoolValue(org.VMScanEnabled),
	})
}

func awsOrganizationAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"is_management_account":  types.BoolType,
		"organization_id":        types.StringType,
		"synced_unit_ids":        types.ListType{ElemType: types.StringType},
		"excluded_accounts":      types.ListType{ElemType: types.StringType},
		"container_scan_enabled": types.BoolType,
		"vm_scan_enabled":        types.BoolType,
	}
}

func identityOnlyAWSCloud(id int64, planned awsCloudModel) awsCloudModel {
	return awsCloudModel{
		ID:            types.StringValue(strconv.FormatInt(id, 10)),
		Name:          planned.Name,
		RoleARN:       planned.RoleARN,
		Environment:   planned.Environment,
		Organization:  planned.Organization,
		ExternalID:    types.StringNull(),
		LastScannedAt: types.Int64Null(),
	}
}

func stringListValues(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}

	values := []string{}
	diagnostics := list.ElementsAs(ctx, &values, false)

	return values, diagnostics
}

func parseCloudID(cloudID string) (int64, error) {
	id, err := strconv.ParseInt(cloudID, 10, 64)

	if err != nil {
		return 0, fmt.Errorf("invalid cloud id %q: %w", cloudID, err)
	}

	return id, nil
}

// roleARNRequiresReplaceWhenOrganization forces replacement when role_arn
// changes on an organization-linked cloud. The public update API does not
// accept a new role for management accounts; silently omitting it left Terraform
// drifted against state after read-back.
func roleARNRequiresReplaceWhenOrganization(ctx context.Context, request planmodifier.StringRequest, response *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	var organization types.Object
	response.Diagnostics.Append(request.State.GetAttribute(ctx, path.Root("organization"), &organization)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.RequiresReplace = !organization.IsNull() && !organization.IsUnknown()
}
