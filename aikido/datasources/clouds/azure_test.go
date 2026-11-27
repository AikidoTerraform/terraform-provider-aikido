package clouds

import (
	"context"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func TestAzureObjectValue(t *testing.T) {
	ctx := context.Background()
	details := &clouds.AzureDetails{
		ApplicationId: "app-1", DirectoryId: "tenant-1", AzureEnvironment: "public",
		IsManagementAccount: true,
		Organization: &clouds.CloudOrganization{
			OrganizationID:  "tenant-1",
			ExcludedFolders: " group-1,group-2 ", ExcludedAccounts: "sub-2",
			ContainerScanEnabled: true, VMScanEnabled: true,
		},
	}
	for _, scope := range []string{"tenant-1", " group-a,group-b "} {
		t.Run(scope, func(t *testing.T) {
			details.Organization.SyncedUnitIDs = scope
			object, diagnostics := AzureObjectValue(ctx, 4, details)
			if diagnostics.HasError() {
				t.Fatal(diagnostics)
			}
			var azure azureModel
			if d := object.As(ctx, &azure, basetypes.ObjectAsOptions{}); d.HasError() {
				t.Fatal(d)
			}
			if !azure.IsManagementAccount.ValueBool() || azure.ApplicationID.ValueString() != "app-1" || azure.DirectoryID.ValueString() != "tenant-1" || azure.AzureEnvironment.ValueString() != "public" {
				t.Fatalf("unexpected Azure model: %#v", azure)
			}
			var org azureOrganizationModel
			if d := azure.Organization.As(ctx, &org, basetypes.ObjectAsOptions{}); d.HasError() {
				t.Fatal(d)
			}
			wantGroups := types.ListValueMust(types.StringType, nil)
			if scope != "tenant-1" {
				wantGroups, diagnostics = types.ListValueFrom(ctx, types.StringType, []string{"group-a", "group-b"})
				if diagnostics.HasError() {
					t.Fatal(diagnostics)
				}
			}
			if !org.ManagementGroupIDs.Equal(wantGroups) || len(org.ExcludedManagementGroupIDs.Elements()) != 2 || len(org.ExcludedSubscriptionIDs.Elements()) != 1 || !org.ACRScanEnabled.ValueBool() || !org.VMScanEnabled.ValueBool() || org.OrganizationID.ValueString() != "tenant-1" {
				t.Fatalf("unexpected organization: %#v", org)
			}
		})
	}

	details.IsManagementAccount = false
	object, diagnostics := AzureObjectValue(ctx, 4, details)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	var azure azureModel
	if d := object.As(ctx, &azure, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if !azure.Organization.IsNull() {
		t.Fatal("member account must not expose management sync settings")
	}
	details.IsManagementAccount = true
	details.Organization = nil
	if _, d := AzureObjectValue(ctx, 4, details); !d.HasError() {
		t.Fatal("expected diagnostic for missing management organization")
	}

	object, diagnostics = AzureObjectValue(ctx, 4, nil)
	if diagnostics.HasError() || !object.Equal(types.ObjectNull(AzureAttributeTypes())) {
		t.Fatalf("expected typed null for absent Azure details: %v, %v", object, diagnostics)
	}
}
