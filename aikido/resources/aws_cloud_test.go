package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/clouds"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func TestAWSCloudModelFromAPI(t *testing.T) {
	t.Run("single account", func(t *testing.T) {
		cloud := clouds.Cloud{
			ID:            7,
			Name:          "AWS production",
			Provider:      clouds.ProviderAWS,
			Environment:   "production",
			ExternalID:    "000000000000",
			LastScannedAt: 1700000000,
			AWS: &clouds.AWSDetails{
				RoleARN: "arn:aws:iam::000000000000:role/aikido",
			},
		}

		model, diagnostics := awsCloudModelFromAPI(context.Background(), cloud)
		if diagnostics.HasError() {
			t.Fatalf("diagnostics: %v", diagnostics)
		}
		if model.ID != types.StringValue("7") {
			t.Errorf("ID = %v", model.ID)
		}
		if model.RoleARN != types.StringValue("arn:aws:iam::000000000000:role/aikido") {
			t.Errorf("RoleARN = %v", model.RoleARN)
		}
		if !model.Organization.IsNull() {
			t.Errorf("Organization = %v, want null", model.Organization)
		}
		if model.IsManagementAccount != types.BoolValue(false) {
			t.Errorf("IsManagementAccount = %v", model.IsManagementAccount)
		}
	})

	t.Run("organization management account", func(t *testing.T) {
		cloud := clouds.Cloud{
			ID:            8,
			Name:          "AWS org",
			Provider:      clouds.ProviderAWS,
			Environment:   "mixed",
			ExternalID:    "111111111111",
			LastScannedAt: -1,
			AWS: &clouds.AWSDetails{
				RoleARN:             "arn:aws:iam::111111111111:role/aikido",
				IsManagementAccount: true,
				Organization: &clouds.Organization{
					OrganizationID:       "o-123",
					SyncedUnitIDs:        "r-abcd",
					ExcludedAccounts:     "222222222222",
					ContainerScanEnabled: true,
					VMScanEnabled:        false,
				},
			},
		}

		model, diagnostics := awsCloudModelFromAPI(context.Background(), cloud)
		if diagnostics.HasError() {
			t.Fatalf("diagnostics: %v", diagnostics)
		}
		if model.Organization.IsNull() {
			t.Fatal("Organization is null, want set")
		}

		var org awsOrganizationModel
		if diagnostics := model.Organization.As(context.Background(), &org, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
			t.Fatalf("Organization.As: %v", diagnostics)
		}
		if org.OrganizationID != types.StringValue("o-123") {
			t.Errorf("organization_id = %v", org.OrganizationID)
		}
		if org.ContainerScanEnabled != types.BoolValue(true) {
			t.Errorf("container_scan_enabled = %v", org.ContainerScanEnabled)
		}
	})
}

func TestCreateAWSInput(t *testing.T) {
	input, diagnostics := createAWSInput(awsCloudModel{
		Name:         types.StringValue("prod"),
		RoleARN:      types.StringValue("arn:aws:iam::1:role/aikido"),
		Environment:  types.StringValue("production"),
		Organization: types.ObjectNull(awsOrganizationAttributeTypes()),
	})
	if diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", diagnostics)
	}
	if input.Name != "prod" || input.RoleARN == "" {
		t.Fatalf("input = %#v", input)
	}
}

func TestCreateAWSOrganizationInput(t *testing.T) {
	org, diagnostics := types.ObjectValueFrom(context.Background(), awsOrganizationAttributeTypes(), awsOrganizationModel{
		OrganizationID:       types.StringValue("o-123"),
		SyncedUnitIDs:        types.ListValueMust(types.StringType, []attr.Value{types.StringValue("r-abcd")}),
		ExcludedAccounts:     types.ListValueMust(types.StringType, []attr.Value{}),
		ContainerScanEnabled: types.BoolValue(true),
		VMScanEnabled:        types.BoolValue(false),
	})
	if diagnostics.HasError() {
		t.Fatalf("ObjectValueFrom: %v", diagnostics)
	}

	input, diagnostics := createAWSOrganizationInput(context.Background(), awsCloudModel{
		Name:         types.StringValue("org"),
		RoleARN:      types.StringValue("arn:aws:iam::1:role/aikido"),
		Environment:  types.StringValue("mixed"),
		Organization: org,
	})
	if diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", diagnostics)
	}
	if input.OrganizationID != "o-123" {
		t.Errorf("organization_id = %q", input.OrganizationID)
	}
	if input.SyncedUnitIDs != "r-abcd" {
		t.Errorf("synced_unit_ids = %q", input.SyncedUnitIDs)
	}
	if !input.ContainerScanEnabled {
		t.Error("container_scan_enabled = false, want true")
	}
}

func TestAWSCloudCreateAndDelete(t *testing.T) {
	listed := clouds.Cloud{
		ID:            42,
		Name:          "AWS production",
		Provider:      clouds.ProviderAWS,
		Environment:   "production",
		ExternalID:    "000000000000",
		LastScannedAt: -1,
		AWS: &clouds.AWSDetails{
			RoleARN: "arn:aws:iam::000000000000:role/aikido",
		},
	}

	var order []string
	var createBody clouds.CreateAWSInput
	var listHadOrgDetails bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == clouds.AWSBasePath:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &createBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int64{"id": listed.ID})
		case r.Method == http.MethodGet && r.URL.Path == clouds.BasePath:
			listHadOrgDetails = r.URL.Query().Get("with_org_details") == "true"
			_ = json.NewEncoder(w).Encode([]clouds.Cloud{listed})
		case r.Method == http.MethodDelete && r.URL.Path == clouds.DetailPath(listed.ID):
			_, _ = io.WriteString(w, `{"success":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	res := &awsCloudResource{client: testClient(srv)}

	input, diagnostics := createAWSInput(awsCloudModel{
		Name:         types.StringValue(listed.Name),
		RoleARN:      types.StringValue(listed.AWS.RoleARN),
		Environment:  types.StringValue(listed.Environment),
		Organization: types.ObjectNull(awsOrganizationAttributeTypes()),
	})
	if diagnostics.HasError() {
		t.Fatalf("createAWSInput: %v", diagnostics)
	}

	id, err := clouds.CreateAWS(context.Background(), res.client, input)
	if err != nil {
		t.Fatalf("CreateAWS: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d", id)
	}
	if createBody.Name != listed.Name || createBody.RoleARN != listed.AWS.RoleARN {
		t.Fatalf("create body = %#v", createBody)
	}

	state, diagnostics := res.readBack(context.Background(), id, awsCloudModel{
		Name:         types.StringValue(listed.Name),
		RoleARN:      types.StringValue(listed.AWS.RoleARN),
		Environment:  types.StringValue(listed.Environment),
		Organization: types.ObjectNull(awsOrganizationAttributeTypes()),
	})
	if diagnostics.HasError() {
		t.Fatalf("readBack: %v", diagnostics)
	}
	if state.ExternalID != types.StringValue(listed.ExternalID) {
		t.Errorf("ExternalID = %v", state.ExternalID)
	}

	if err := clouds.Delete(context.Background(), res.client, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(order) < 3 || order[0] != "POST "+clouds.AWSBasePath {
		t.Fatalf("call order = %v", order)
	}
	if !listHadOrgDetails {
		t.Error("list GET missing with_org_details=true")
	}
}
