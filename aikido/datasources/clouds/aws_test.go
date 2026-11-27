package clouds

import (
	"context"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func TestAWSObjectValue(t *testing.T) {
	ctx := context.Background()
	details := &clouds.AWSDetails{
		RoleARN:             "arn:aws:iam::111111111111:role/aikido",
		IsManagementAccount: true,
		Organization: &clouds.CloudOrganization{
			OrganizationID: "o-123",
			SyncedUnitIDs:  "r-abcd",
		},
	}
	object, diagnostics := AWSObjectValue(ctx, details)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	var aws awsModel
	if d := object.As(ctx, &aws, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if !aws.IsManagementAccount.ValueBool() || aws.RoleARN.ValueString() != details.RoleARN || aws.Organization.IsNull() {
		t.Fatalf("unexpected AWS model: %#v", aws)
	}
	var org awsOrganizationModel
	if d := aws.Organization.As(ctx, &org, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if org.OrganizationID != types.StringValue("o-123") {
		t.Fatalf("organization_id = %v", org.OrganizationID)
	}

	details.IsManagementAccount = false
	details.Organization = nil
	object, diagnostics = AWSObjectValue(ctx, details)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if d := object.As(ctx, &aws, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if !aws.Organization.IsNull() {
		t.Fatal("expected organization to be null for single-account clouds")
	}

	object, diagnostics = AWSObjectValue(ctx, nil)
	if diagnostics.HasError() || !object.Equal(types.ObjectNull(AWSAttributeTypes())) {
		t.Fatalf("expected typed null for absent AWS details: %v, %v", object, diagnostics)
	}
}
