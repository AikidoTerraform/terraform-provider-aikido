package datasources

import (
	"context"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func awsCloud() clouds.Cloud {
	return clouds.Cloud{
		ID: 1, Name: "AWS production", Provider: clouds.ProviderAWS, Environment: "production",
		ExternalID: "000000000000",
		AWS: &clouds.AWSDetails{
			RoleARN: "arn:aws:iam::000000000000:role/aikido",
		},
	}
}

func awsOrgCloud() clouds.Cloud {
	return clouds.Cloud{
		ID: 2, Name: "AWS org", Provider: clouds.ProviderAWS, Environment: "mixed",
		ExternalID: "111111111111",
		AWS: &clouds.AWSDetails{
			RoleARN:             "arn:aws:iam::111111111111:role/aikido",
			IsManagementAccount: true,
			Organization: &clouds.Organization{
				OrganizationID: "o-123",
				SyncedUnitIDs:  "r-abcd",
			},
		},
	}
}

func azureCloud() clouds.Cloud {
	return clouds.Cloud{
		ID: 3, Name: "Azure prod", Provider: "azure", Environment: "production",
		ExternalID: "sub-1",
	}
}

func allTestClouds() []clouds.Cloud {
	return []clouds.Cloud{awsCloud(), awsOrgCloud(), azureCloud()}
}

func supportedTestClouds() []clouds.Cloud {
	all := allTestClouds()
	supported := make([]clouds.Cloud, 0, len(all))
	for _, cloud := range all {
		if clouds.IsSupported(cloud.Provider) {
			supported = append(supported, cloud)
		}
	}
	return supported
}

func matchedCloudIDs(config cloudsDataSourceModel) []int64 {
	_, ids, diagnostics := matchingClouds(context.Background(), supportedTestClouds(), config)
	if diagnostics.HasError() {
		panic(diagnostics.Errors())
	}

	plain := make([]int64, 0, len(ids))
	for _, id := range ids {
		plain = append(plain, id.ValueInt64())
	}
	return plain
}

func TestMatchingClouds_Filters(t *testing.T) {
	tests := []struct {
		name    string
		config  cloudsDataSourceModel
		wantIDs []int64
	}{
		{
			name:    "no filter returns every supported cloud",
			config:  cloudsDataSourceModel{},
			wantIDs: []int64{1, 2},
		},
		{
			name:    "cloud_provider selects aws",
			config:  cloudsDataSourceModel{CloudProvider: types.StringValue(clouds.ProviderAWS)},
			wantIDs: []int64{1, 2},
		},
		{
			name:    "management account filter",
			config:  cloudsDataSourceModel{IsManagementAccount: types.BoolValue(true)},
			wantIDs: []int64{2},
		},
		{
			name:    "name matches exactly",
			config:  cloudsDataSourceModel{Name: types.StringValue("AWS production")},
			wantIDs: []int64{1},
		},
		{
			name:    "unsupported provider clouds are not listed",
			config:  cloudsDataSourceModel{Name: types.StringValue("Azure prod")},
			wantIDs: []int64{},
		},
		{
			name:    "external_id matches",
			config:  cloudsDataSourceModel{ExternalID: types.StringValue("000000000000")},
			wantIDs: []int64{1},
		},
		{
			name: "filters combine with AND",
			config: cloudsDataSourceModel{
				CloudProvider: types.StringValue(clouds.ProviderAWS),
				Environment:   types.StringValue("mixed"),
			},
			wantIDs: []int64{2},
		},
		{
			name:    "no match yields empty",
			config:  cloudsDataSourceModel{Environment: types.StringValue("staging")},
			wantIDs: []int64{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := matchedCloudIDs(test.config)
			if len(got) != len(test.wantIDs) {
				t.Fatalf("ids = %v, want %v", got, test.wantIDs)
			}
			for i := range got {
				if got[i] != test.wantIDs[i] {
					t.Fatalf("ids = %v, want %v", got, test.wantIDs)
				}
			}
		})
	}
}

func TestCloudModelFromAPI_NestsAWS(t *testing.T) {
	model, diagnostics := cloudModelFromAPI(context.Background(), awsOrgCloud())
	if diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", diagnostics)
	}
	if model.AWS.IsNull() {
		t.Fatal("expected aws nested object")
	}

	var aws cloudAWSModel
	diagnostics.Append(model.AWS.As(context.Background(), &aws, basetypes.ObjectAsOptions{})...)
	if diagnostics.HasError() {
		t.Fatalf("aws as: %v", diagnostics)
	}
	if !aws.IsManagementAccount.ValueBool() {
		t.Fatalf("is_management_account = %v", aws.IsManagementAccount)
	}
	if aws.Organization.IsNull() {
		t.Fatal("expected organization nested object on management account")
	}

	var org cloudAWSOrganizationModel
	diagnostics.Append(aws.Organization.As(context.Background(), &org, basetypes.ObjectAsOptions{})...)
	if diagnostics.HasError() {
		t.Fatalf("organization as: %v", diagnostics)
	}
	if org.OrganizationID != types.StringValue("o-123") {
		t.Fatalf("organization_id = %v", org.OrganizationID)
	}

	singleModel, diagnostics := cloudModelFromAPI(context.Background(), awsCloud())
	if diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", diagnostics)
	}
	var singleAWS cloudAWSModel
	diagnostics.Append(singleModel.AWS.As(context.Background(), &singleAWS, basetypes.ObjectAsOptions{})...)
	if diagnostics.HasError() {
		t.Fatalf("aws as: %v", diagnostics)
	}
	if !singleAWS.Organization.IsNull() {
		t.Fatal("expected organization to be null for single-account clouds")
	}

	azureModel, diagnostics := cloudModelFromAPI(context.Background(), azureCloud())
	if diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", diagnostics)
	}
	if !azureModel.AWS.IsNull() {
		t.Fatal("expected aws nested object to be null for non-aws clouds")
	}
}
