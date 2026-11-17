package clouds

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const (
	ProviderAWS = "aws"
	AWSBasePath = "/public/v1/clouds/aws"
	AWSOrgPath  = "/public/v1/clouds/aws/organization"
)

// AWSDetails holds AWS-specific fields. On the public API they are flat on the
// cloud object; Cloud.UnmarshalJSON / MarshalJSON project them here.
type AWSDetails struct {
	RoleARN             string        `json:"role_arn"`
	IsManagementAccount bool          `json:"is_management_account"`
	Organization        *Organization `json:"organization"`
}

func (a AWSDetails) empty() bool {
	return a.RoleARN == "" && !a.IsManagementAccount && a.Organization == nil
}

// Organization is the organization block returned on GET/list for an AWS
// management-account cloud. SyncedUnitIDs and ExcludedAccounts are
// comma-separated IDs (e.g. "ou-a,ou-b").
type Organization struct {
	OrganizationID       string `json:"organization_id"`
	SyncedUnitIDs        string `json:"synced_unit_ids"`
	ExcludedAccounts     string `json:"excluded_accounts"`
	ContainerScanEnabled bool   `json:"container_scan_enabled"`
	VMScanEnabled        bool   `json:"vm_scan_enabled"`
}

func SplitCommaSeparated(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part != "" {
			out = append(out, part)
		}
	}

	return out
}

// CreateAWSInput is the body for POST /clouds/aws (single account).
type CreateAWSInput struct {
	Name        string `json:"name"`
	RoleARN     string `json:"role_arn"`
	Environment string `json:"environment"`
}

// CreateAWSOrganizationInput is the body for POST /clouds/aws/organization.
// Fields are flat on the request, matching the public API.
type CreateAWSOrganizationInput struct {
	Name                 string `json:"name"`
	RoleARN              string `json:"role_arn"`
	Environment          string `json:"environment"`
	OrganizationID       string `json:"organization_id"`
	SyncedUnitIDs        string `json:"synced_unit_ids"`
	ExcludedAccounts     string `json:"excluded_accounts,omitempty"`
	ContainerScanEnabled bool   `json:"container_scan_enabled"`
	VMScanEnabled        bool   `json:"vm_scan_enabled"`
}

// UpdateAWSInput is the body for PUT /clouds/aws/{id}. Omitted pointer fields
// are left unchanged.
type UpdateAWSInput struct {
	Name         *string             `json:"name,omitempty"`
	RoleARN      *string             `json:"role_arn,omitempty"`
	Environment  *string             `json:"environment,omitempty"`
	Organization *UpdateOrganization `json:"organization,omitempty"`
}

// UpdateOrganization updates sync settings for a management account.
type UpdateOrganization struct {
	SyncedUnitIDs    string `json:"synced_unit_ids,omitempty"`
	ExcludedAccounts string `json:"excluded_accounts"`
}

// CreateAWS connects a single AWS account and returns its new ID.
func CreateAWS(ctx context.Context, apiClient *client.Client, input CreateAWSInput) (int64, error) {
	defer client.InvalidateCached(apiClient, cacheKey)

	var created struct {
		ID int64 `json:"id"`
	}

	if err := apiClient.Do(ctx, http.MethodPost, AWSBasePath, input, &created); err != nil {
		return 0, err
	}

	return created.ID, nil
}

// CreateAWSOrganization connects an AWS Organization via its management account
// and returns its new ID.
func CreateAWSOrganization(ctx context.Context, apiClient *client.Client, input CreateAWSOrganizationInput) (int64, error) {
	defer client.InvalidateCached(apiClient, cacheKey)

	var created struct {
		ID int64 `json:"id"`
	}

	if err := apiClient.Do(ctx, http.MethodPost, AWSOrgPath, input, &created); err != nil {
		return 0, err
	}

	return created.ID, nil
}

// UpdateAWS updates an AWS cloud connection.
func UpdateAWS(ctx context.Context, apiClient *client.Client, id int64, input UpdateAWSInput) error {
	defer client.InvalidateCached(apiClient, cacheKey)
	return apiClient.Do(ctx, http.MethodPut, AWSBasePath+"/"+strconv.FormatInt(id, 10), input, nil)
}
