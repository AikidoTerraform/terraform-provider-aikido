package datasources

import (
	"context"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
			Organization: &clouds.CloudOrganization{
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
		Azure:      &clouds.AzureDetails{ApplicationId: "app-1", DirectoryId: "tenant-1", AzureEnvironment: "public"},
	}
}

func azureOrgCloud() clouds.Cloud {
	cloud := azureCloud()
	cloud.ID = 4
	cloud.Name = "Azure management"
	cloud.Azure.IsManagementAccount = true
	cloud.Azure.Organization = &clouds.CloudOrganization{
		OrganizationID: "tenant-1", SyncedUnitIDs: "tenant-1",
		ExcludedFolders: " group-1,group-2 ", ExcludedAccounts: "sub-2",
		ContainerScanEnabled: true, VMScanEnabled: true,
	}
	return cloud
}

func allTestClouds() []clouds.Cloud {
	return []clouds.Cloud{awsCloud(), awsOrgCloud(), azureCloud(), azureOrgCloud(), {ID: 5, Name: "GCP prod", Provider: "gcp"}}
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
			wantIDs: []int64{1, 2, 3, 4},
		},
		{
			name:    "cloud_provider selects aws",
			config:  cloudsDataSourceModel{CloudProvider: types.StringValue(clouds.ProviderAWS)},
			wantIDs: []int64{1, 2},
		},
		{
			name:    "management account filter",
			config:  cloudsDataSourceModel{IsManagementAccount: types.BoolValue(true)},
			wantIDs: []int64{2, 4},
		},
		{
			name:    "non-management account filter",
			config:  cloudsDataSourceModel{IsManagementAccount: types.BoolValue(false)},
			wantIDs: []int64{1, 3},
		},
		{
			name:    "azure management account filter",
			config:  cloudsDataSourceModel{CloudProvider: types.StringValue(clouds.ProviderAzure), IsManagementAccount: types.BoolValue(true)},
			wantIDs: []int64{4},
		},
		{
			name:    "name matches exactly",
			config:  cloudsDataSourceModel{Name: types.StringValue("AWS production")},
			wantIDs: []int64{1},
		},
		{
			name:    "unsupported provider clouds are not listed",
			config:  cloudsDataSourceModel{Name: types.StringValue("GCP prod")},
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

func TestCloudsDataSourceStateMatchesSchema(t *testing.T) {
	ctx := context.Background()
	d := NewCloudsDataSource()
	var response datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &response)
	if diagnostics := response.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	models, ids, diagnostics := matchingClouds(ctx, supportedTestClouds(), cloudsDataSourceModel{})
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	state := tfsdk.State{Schema: response.Schema}
	if diagnostics := state.Set(ctx, cloudsDataSourceModel{Clouds: models, IDs: ids}); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	var roundTrip cloudsDataSourceModel
	if diagnostics := state.Get(ctx, &roundTrip); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if len(roundTrip.Clouds) != 4 || !roundTrip.Clouds[0].Azure.IsNull() || roundTrip.Clouds[3].Azure.IsNull() || roundTrip.Clouds[0].AWS.IsNull() || !roundTrip.Clouds[3].AWS.IsNull() {
		t.Fatalf("unexpected round-trip state: %#v", roundTrip)
	}
}
