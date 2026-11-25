package clouds

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"golang.org/x/time/rate"
)

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

func TestCloudJSONRoundTrip(t *testing.T) {
	raw := []byte(`{
		"id": 1,
		"name": "prod",
		"provider": "aws",
		"environment": "production",
		"external_id": "000000000000",
		"last_scanned_at": -1,
		"role_arn": "arn:aws:iam::000000000000:role/aikido",
		"is_management_account": false
	}`)

	var cloud Cloud
	if err := json.Unmarshal(raw, &cloud); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cloud.Name != "prod" || cloud.AWS == nil || cloud.AWS.RoleARN == "" {
		t.Fatalf("cloud = %#v", cloud)
	}

	encoded, err := json.Marshal(cloud)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var flat map[string]any
	if err := json.Unmarshal(encoded, &flat); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if flat["role_arn"] != "arn:aws:iam::000000000000:role/aikido" {
		t.Fatalf("flat = %#v", flat)
	}
	if _, ok := flat["aws"]; ok {
		t.Fatalf("aws key should not be present: %#v", flat)
	}
}

func TestSplitCommaSeparated(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"  ", []string{}},
		{"r-abcd", []string{"r-abcd"}},
		{"r-abcd,ou-1", []string{"r-abcd", "ou-1"}},
		{" r-abcd , ou-1 , ", []string{"r-abcd", "ou-1"}},
	}

	for _, test := range tests {
		got := SplitCommaSeparated(test.in)
		if len(got) != len(test.want) {
			t.Fatalf("SplitCommaSeparated(%q) = %v, want %v", test.in, got, test.want)
		}
		for i := range got {
			if got[i] != test.want[i] {
				t.Fatalf("SplitCommaSeparated(%q) = %v, want %v", test.in, got, test.want)
			}
		}
	}
}

func TestCreateAWSAndList(t *testing.T) {
	var createBody CreateAWSInput
	var listQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == AWSBasePath:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &createBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int64{"id": 9})
		case r.Method == http.MethodGet && r.URL.Path == BasePath:
			listQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode([]Cloud{{
				ID: 9, Name: createBody.Name, Provider: ProviderAWS, Environment: createBody.Environment,
				ExternalID: "000000000000", LastScannedAt: -1,
				AWS: &AWSDetails{RoleARN: createBody.RoleARN},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	apiClient := testClient(srv)

	id, err := CreateAWS(context.Background(), apiClient, CreateAWSInput{
		Name: "prod", RoleARN: "arn:aws:iam::000000000000:role/aikido", Environment: "production",
	})
	if err != nil {
		t.Fatalf("CreateAWS: %v", err)
	}
	if id != 9 {
		t.Fatalf("id = %d", id)
	}

	cloud, err := ByID(context.Background(), apiClient, 9)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if cloud.Name != "prod" {
		t.Errorf("Name = %q", cloud.Name)
	}
	if got := listQuery; !strings.Contains(got, "org_details=true") {
		t.Errorf("list query = %q, want org_details=true", got)
	}
}

func TestCreateAWSOrganization(t *testing.T) {
	var createBody CreateAWSOrganizationInput

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != AWSOrgPath {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &createBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]int64{"id": 11})
	}))
	t.Cleanup(srv.Close)

	id, err := CreateAWSOrganization(context.Background(), testClient(srv), CreateAWSOrganizationInput{
		Name: "org", RoleARN: "arn:aws:iam::1:role/aikido", Environment: "mixed",
		OrganizationID: "o-1", SyncedUnitIDs: "r-1", ContainerScanEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAWSOrganization: %v", err)
	}
	if id != 11 {
		t.Fatalf("id = %d", id)
	}
	if createBody.OrganizationID != "o-1" || createBody.SyncedUnitIDs != "r-1" {
		t.Fatalf("create body = %#v", createBody)
	}
}

func TestGet_RequestsOrgDetails(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != DetailPath(12) {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(Cloud{
			ID: 12, Name: "org", Provider: ProviderAWS, Environment: "mixed",
			ExternalID: "111111111111", LastScannedAt: -1,
			AWS: &AWSDetails{
				RoleARN:             "arn:aws:iam::111111111111:role/aikido",
				IsManagementAccount: true,
				Organization: &CloudOrganization{
					OrganizationID: "o-1", SyncedUnitIDs: "r-1",
					ContainerScanEnabled: true,
					VMScanEnabled:        true,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	cloud, err := Get(context.Background(), testClient(srv), 12)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotQuery != listQueryParams {
		t.Errorf("query = %q, want %q", gotQuery, listQueryParams)
	}
	if cloud.AWS == nil || cloud.AWS.Organization == nil || cloud.AWS.Organization.OrganizationID != "o-1" {
		t.Fatalf("cloud = %#v", cloud)
	}
}
