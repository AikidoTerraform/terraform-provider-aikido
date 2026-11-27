package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func TestAzureCloudLifecycle(t *testing.T) {
	for _, management := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscription", true: "management"}[management], func(t *testing.T) {
			ctx := context.Background()
			planned := AzureCloudModel{
				ID: types.StringUnknown(), Name: types.StringValue("Azure production"),
				ApplicationID: types.StringValue("app-id"), DirectoryID: types.StringValue("tenant-id"),
				SubscriptionID: types.StringValue("subscription-id"), KeyValue: types.StringValue("test-secret"),
				Environment: types.StringValue("production"), AzureEnvironment: types.StringValue("gov"),
				Organization: types.ObjectNull(AzureOrganizationAttributeTypes()),
				ExternalID:   types.StringUnknown(), LastScannedAt: types.Int64Unknown(),
			}
			listed := clouds.Cloud{
				ID: 42, Name: "Azure production", Provider: "azure", Environment: "production",
				ExternalID: "subscription-id", LastScannedAt: -1,
				Azure: &clouds.AzureDetails{ApplicationId: "app-id", DirectoryId: "tenant-id", AzureEnvironment: "gov"},
			}
			expectedCreate := map[string]any{
				"name": "Azure production", "application_id": "app-id", "directory_id": "tenant-id",
				"key_value": "test-secret", "environment": "production", "azure_environment": "gov",
			}
			createPath := clouds.AzureBasePath
			if management {
				createPath = clouds.AzureOrgPath
				expectedCreate["management_subscription_id"] = "subscription-id"
				expectedCreate["excluded_management_group_ids"] = "excluded-group"
				expectedCreate["excluded_subscription_ids"] = "excluded-subscription"
				expectedCreate["acr_scan_enabled"] = true
				org := AzureOrganizationModel{
					IsManagementAccount: types.BoolUnknown(), OrganizationID: types.StringUnknown(),
					ManagementGroupIDs:         types.ListValueMust(types.StringType, []attr.Value{}),
					ExcludedManagementGroupIDs: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("excluded-group")}),
					ExcludedSubscriptionIDs:    types.ListValueMust(types.StringType, []attr.Value{types.StringValue("excluded-subscription")}),
					ACRScanEnabled:             types.BoolValue(true), VMScanEnabled: types.BoolValue(false),
				}
				object, diagnostics := types.ObjectValueFrom(ctx, AzureOrganizationAttributeTypes(), org)
				if diagnostics.HasError() {
					t.Fatal(diagnostics)
				}
				planned.Organization = object
				listed.Azure.IsManagementAccount = true
				listed.Azure.Organization = &clouds.CloudOrganization{
					OrganizationID: "tenant-id", SyncedUnitIDs: "tenant-id", ExcludedFolders: "excluded-group",
					ExcludedAccounts: "excluded-subscription", ContainerScanEnabled: true,
				}
			} else {
				expectedCreate["subscription_id"] = "subscription-id"
			}
			var createBody, credentialsBody, organizationBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch {
				case req.Method == http.MethodPost && req.URL.Path == createPath:
					if err := json.NewDecoder(req.Body).Decode(&createBody); err != nil {
						t.Error(err)
					}
					key := "id"
					if management {
						key = "cloud_id"
					}
					_ = json.NewEncoder(w).Encode(map[string]int64{key: 42})
				case req.Method == http.MethodGet && req.URL.Path == clouds.BasePath:
					if req.URL.Query().Get("org_details") != "true" {
						t.Error("missing org_details")
					}
					_ = json.NewEncoder(w).Encode([]clouds.Cloud{listed})
				case req.Method == http.MethodPut && req.URL.Path == clouds.AzureBasePath+"/42/credentials":
					if err := json.NewDecoder(req.Body).Decode(&credentialsBody); err != nil {
						t.Error(err)
					}
				case req.Method == http.MethodPut && req.URL.Path == clouds.DetailPath(42)+"/organization":
					if err := json.NewDecoder(req.Body).Decode(&organizationBody); err != nil {
						t.Error(err)
					}
				case req.Method == http.MethodPut && req.URL.Path == clouds.DetailPath(42):
				case req.Method == http.MethodDelete && req.URL.Path == clouds.DetailPath(42):
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
					http.NotFound(w, req)
				}
			}))
			t.Cleanup(srv.Close)
			r := &AzureCloudResource{client: testClient(srv)}
			schemaResponse := resource.SchemaResponse{}
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
			if d := schemaResponse.Schema.ValidateImplementation(ctx); d.HasError() {
				t.Fatal(d)
			}
			plan := tfsdk.Plan{Schema: schemaResponse.Schema}
			if d := plan.Set(ctx, planned); d.HasError() {
				t.Fatal(d)
			}
			created := resource.CreateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
			if created.Diagnostics.HasError() {
				t.Fatal(created.Diagnostics)
			}
			if !reflect.DeepEqual(createBody, expectedCreate) {
				t.Fatalf("create body = %#v, want %#v", createBody, expectedCreate)
			}
			var state AzureCloudModel
			if d := created.State.Get(ctx, &state); d.HasError() {
				t.Fatal(d)
			}
			if state.ID.ValueString() != "42" || !state.KeyValue.Equal(planned.KeyValue) {
				t.Fatalf("state = %#v", state)
			}
			if management {
				var org AzureOrganizationModel
				if d := state.Organization.As(ctx, &org, basetypes.ObjectAsOptions{}); d.HasError() {
					t.Fatal(d)
				}
				if len(org.ManagementGroupIDs.Elements()) != 0 {
					t.Fatalf("tenant root not normalized: %v", org.ManagementGroupIDs)
				}
			}
			read := resource.ReadResponse{State: created.State}
			r.Read(ctx, resource.ReadRequest{State: created.State}, &read)
			if read.Diagnostics.HasError() {
				t.Fatal(read.Diagnostics)
			}
			if d := read.State.Get(ctx, &state); d.HasError() {
				t.Fatal(d)
			}
			if !state.KeyValue.Equal(planned.KeyValue) {
				t.Fatal("refresh lost client secret")
			}
			state.KeyValue = types.StringValue("rotated-test-secret")
			if d := plan.Set(ctx, state); d.HasError() {
				t.Fatal(d)
			}
			updated := resource.UpdateResponse{State: read.State}
			r.Update(ctx, resource.UpdateRequest{Plan: plan, State: read.State}, &updated)
			if updated.Diagnostics.HasError() {
				t.Fatal(updated.Diagnostics)
			}
			if !reflect.DeepEqual(credentialsBody, map[string]any{"client_secret": "rotated-test-secret"}) {
				t.Fatalf("credentials body = %#v", credentialsBody)
			}
			if management && !reflect.DeepEqual(organizationBody, map[string]any{"synced_unit_ids": "", "excluded_accounts": "excluded-subscription", "excluded_folders": "excluded-group"}) {
				t.Fatalf("organization body = %#v", organizationBody)
			}
			if d := updated.State.Get(ctx, &state); d.HasError() {
				t.Fatal(d)
			}
			if state.KeyValue.ValueString() != "rotated-test-secret" {
				t.Fatal("update lost rotated secret")
			}
			deleted := resource.DeleteResponse{}
			r.Delete(ctx, resource.DeleteRequest{State: updated.State}, &deleted)
			if deleted.Diagnostics.HasError() {
				t.Fatal(deleted.Diagnostics)
			}
		})
	}
}

func TestAzureCloudModelRejectsInvalidDetails(t *testing.T) {
	for _, cloud := range []clouds.Cloud{
		{ID: 1, Provider: clouds.ProviderAWS},
		{ID: 2, Provider: clouds.ProviderAzure},
		{ID: 3, Provider: clouds.ProviderAzure, Azure: &clouds.AzureDetails{IsManagementAccount: true}},
	} {
		if _, d := AzureCloudModelFromAPI(context.Background(), cloud); !d.HasError() {
			t.Errorf("expected error for %#v", cloud)
		}
	}
}
