package clouds

import (
	"context"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type awsModel struct {
	RoleARN             types.String `tfsdk:"role_arn"`
	IsManagementAccount types.Bool   `tfsdk:"is_management_account"`
	Organization        types.Object `tfsdk:"organization"`
}

type awsOrganizationModel struct {
	OrganizationID       types.String `tfsdk:"organization_id"`
	SyncedUnitIDs        types.List   `tfsdk:"synced_unit_ids"`
	ExcludedAccounts     types.List   `tfsdk:"excluded_accounts"`
	ContainerScanEnabled types.Bool   `tfsdk:"container_scan_enabled"`
	VMScanEnabled        types.Bool   `tfsdk:"vm_scan_enabled"`
}

func AWSSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Computed:    true,
		Description: "AWS-specific attributes. Null when cloud_provider is not aws.",
		Attributes: map[string]schema.Attribute{
			"role_arn": schema.StringAttribute{
				Computed:    true,
				Description: "IAM role ARN Aikido assumes to scan this account.",
			},
			"is_management_account": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this cloud is an organization management account.",
			},
			"organization": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "AWS Organization sync settings when this cloud is a management account. Null for single-account connections.",
				Attributes: map[string]schema.Attribute{
					"organization_id": schema.StringAttribute{
						Computed:    true,
						Description: "AWS Organizations ID, for example o-1234567890.",
					},
					"synced_unit_ids": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "Root ID (r-...) or Organization Unit IDs (ou-...) included in organization sync.",
					},
					"excluded_accounts": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						Description: "AWS account IDs excluded from organization sync.",
					},
					"container_scan_enabled": schema.BoolAttribute{
						Computed:    true,
						Description: "Whether organization-wide ECR scanning is enabled.",
					},
					"vm_scan_enabled": schema.BoolAttribute{
						Computed:    true,
						Description: "Whether organization-wide EBS/EC2 scanning is enabled.",
					},
				},
			},
		},
	}
}

func AWSAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"role_arn":              types.StringType,
		"is_management_account": types.BoolType,
		"organization":          types.ObjectType{AttrTypes: awsOrganizationAttributeTypes()},
	}
}

func awsOrganizationAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"organization_id":        types.StringType,
		"synced_unit_ids":        types.ListType{ElemType: types.StringType},
		"excluded_accounts":      types.ListType{ElemType: types.StringType},
		"container_scan_enabled": types.BoolType,
		"vm_scan_enabled":        types.BoolType,
	}
}

func AWSObjectValue(ctx context.Context, details *clouds.AWSDetails) (types.Object, diag.Diagnostics) {
	if details == nil {
		return types.ObjectNull(AWSAttributeTypes()), nil
	}

	var diagnostics diag.Diagnostics
	model := awsModel{
		RoleARN:             types.StringValue(details.RoleARN),
		IsManagementAccount: types.BoolValue(details.IsManagementAccount),
		Organization:        types.ObjectNull(awsOrganizationAttributeTypes()),
	}
	if details.Organization != nil {
		orgObject, orgDiagnostics := awsOrganizationObjectValue(ctx, *details.Organization)
		diagnostics.Append(orgDiagnostics...)
		if diagnostics.HasError() {
			return types.ObjectNull(AWSAttributeTypes()), diagnostics
		}
		model.Organization = orgObject
	}

	object, objectDiagnostics := types.ObjectValueFrom(ctx, AWSAttributeTypes(), model)
	diagnostics.Append(objectDiagnostics...)
	return object, diagnostics
}

func awsOrganizationObjectValue(ctx context.Context, org clouds.CloudOrganization) (types.Object, diag.Diagnostics) {
	synced, diagnostics := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.SyncedUnitIDs))
	excluded, excludedDiagnostics := types.ListValueFrom(ctx, types.StringType, clouds.SplitCommaSeparated(org.ExcludedAccounts))
	diagnostics.Append(excludedDiagnostics...)
	if diagnostics.HasError() {
		return types.ObjectNull(awsOrganizationAttributeTypes()), diagnostics
	}

	return types.ObjectValueFrom(ctx, awsOrganizationAttributeTypes(), awsOrganizationModel{
		OrganizationID:       types.StringValue(org.OrganizationID),
		SyncedUnitIDs:        synced,
		ExcludedAccounts:     excluded,
		ContainerScanEnabled: types.BoolValue(org.ContainerScanEnabled),
		VMScanEnabled:        types.BoolValue(org.VMScanEnabled),
	})
}
