package clouds

import (
	"context"
	"fmt"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type azureModel struct {
	ApplicationID       types.String `tfsdk:"application_id"`
	DirectoryID         types.String `tfsdk:"directory_id"`
	AzureEnvironment    types.String `tfsdk:"azure_environment"`
	IsManagementAccount types.Bool   `tfsdk:"is_management_account"`
	Organization        types.Object `tfsdk:"organization"`
}

type azureOrganizationModel struct {
	OrganizationID             types.String `tfsdk:"organization_id"`
	ManagementGroupIDs         types.List   `tfsdk:"management_group_ids"`
	ExcludedManagementGroupIDs types.List   `tfsdk:"excluded_management_group_ids"`
	ExcludedSubscriptionIDs    types.List   `tfsdk:"excluded_subscription_ids"`
	ACRScanEnabled             types.Bool   `tfsdk:"acr_scan_enabled"`
	VMScanEnabled              types.Bool   `tfsdk:"vm_scan_enabled"`
}

func AzureSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Computed:    true,
		Description: "Azure-specific attributes. Null when cloud_provider is not azure.",
		Attributes: map[string]schema.Attribute{
			"application_id": schema.StringAttribute{
				Computed:    true,
				Description: "Application (client) ID of the registered application.",
			},
			"directory_id": schema.StringAttribute{
				Computed:    true,
				Description: "Azure directory (tenant) ID.",
			},
			"azure_environment": schema.StringAttribute{
				Computed:    true,
				Description: "Azure cloud environment: public, gov, or china.",
			},
			"is_management_account": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this cloud is an organization management account.",
			},
			"organization": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Azure organization sync settings when this cloud is a management account. Null for member and single-subscription connections.",
				Attributes: map[string]schema.Attribute{
					"organization_id": schema.StringAttribute{
						Computed:    true,
						Description: "Azure directory ID of the connected organization.",
					},
					"management_group_ids": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "Management group IDs to sync. An empty list means tenant root scope.",
					},
					"excluded_management_group_ids": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "Management group IDs excluded from sync.",
					},
					"excluded_subscription_ids": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "Subscription IDs excluded from sync.",
					},
					"acr_scan_enabled": schema.BoolAttribute{
						Computed:    true,
						Description: "Whether organization-wide Azure Container Registry scanning is enabled.",
					},
					"vm_scan_enabled": schema.BoolAttribute{
						Computed:    true,
						Description: "Whether organization-wide VM scanning is enabled.",
					},
				},
			},
		},
	}
}

func AzureAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"application_id":        types.StringType,
		"directory_id":          types.StringType,
		"azure_environment":     types.StringType,
		"is_management_account": types.BoolType,
		"organization":          types.ObjectType{AttrTypes: azureOrganizationAttributeTypes()},
	}
}

func azureOrganizationAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"organization_id":               types.StringType,
		"management_group_ids":          types.ListType{ElemType: types.StringType},
		"excluded_management_group_ids": types.ListType{ElemType: types.StringType},
		"excluded_subscription_ids":     types.ListType{ElemType: types.StringType},
		"acr_scan_enabled":              types.BoolType,
		"vm_scan_enabled":               types.BoolType,
	}
}

func AzureObjectValue(ctx context.Context, cloudID int64, details *clouds.AzureDetails) (types.Object, diag.Diagnostics) {
	if details == nil {
		return types.ObjectNull(AzureAttributeTypes()), nil
	}

	var diagnostics diag.Diagnostics
	model := azureModel{
		ApplicationID:       types.StringValue(details.ApplicationId),
		DirectoryID:         types.StringValue(details.DirectoryId),
		AzureEnvironment:    types.StringValue(details.AzureEnvironment),
		IsManagementAccount: types.BoolValue(details.IsManagementAccount),
		Organization:        types.ObjectNull(azureOrganizationAttributeTypes()),
	}
	if details.IsManagementAccount {
		if details.Organization == nil {
			diagnostics.AddError("Missing Azure organization details", fmt.Sprintf("Cloud %d is a management account but the API returned no organization.", cloudID))
			return types.ObjectNull(AzureAttributeTypes()), diagnostics
		}

		org := details.Organization
		groups, d := types.ListValueFrom(ctx, types.StringType, clouds.AzureManagementGroupIDs(org.SyncedUnitIDs, details.DirectoryId))
		diagnostics.Append(d...)
		excludedGroups, d := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.ExcludedFolders))
		diagnostics.Append(d...)
		excludedSubscriptions, d := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.ExcludedAccounts))
		diagnostics.Append(d...)
		if diagnostics.HasError() {
			return types.ObjectNull(AzureAttributeTypes()), diagnostics
		}

		model.Organization, d = types.ObjectValueFrom(ctx, azureOrganizationAttributeTypes(), azureOrganizationModel{
			OrganizationID:             types.StringValue(org.OrganizationID),
			ManagementGroupIDs:         groups,
			ExcludedManagementGroupIDs: excludedGroups,
			ExcludedSubscriptionIDs:    excludedSubscriptions,
			ACRScanEnabled:             types.BoolValue(org.ContainerScanEnabled),
			VMScanEnabled:              types.BoolValue(org.VMScanEnabled),
		})
		diagnostics.Append(d...)
	}

	object, objectDiagnostics := types.ObjectValueFrom(ctx, AzureAttributeTypes(), model)
	diagnostics.Append(objectDiagnostics...)
	return object, diagnostics
}
