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
	RoleARN             string             `json:"role_arn"`
	IsManagementAccount bool               `json:"is_management_account"`
	Organization        *CloudOrganization `json:"organization"`
}

func (a AWSDetails) empty() bool {
	return a.RoleARN == "" && !a.IsManagementAccount && a.Organization == nil
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

type CreateAWSInput struct {
	Name        string `json:"name"`
	RoleARN     string `json:"role_arn"`
	Environment string `json:"environment"`
}

type CreateAWSOrganizationInput struct {
	Name                 string `json:"name"`
	Environment          string `json:"environment"`
	OrganizationID       string `json:"organization_id"`
	SyncedUnitIDs        string `json:"synced_unit_ids"`
	RoleARN              string `json:"role_arn"`
	ExcludedAccounts     string `json:"excluded_accounts,omitempty"`
	ContainerScanEnabled bool   `json:"container_scan_enabled,omitempty"`
	VMScanEnabled        bool   `json:"vm_scan_enabled,omitempty"`
}

type UpdateAwsCloudCredentialsInput struct {
	RoleARN string `json:"role_arn"`
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

// Update the IAM role ARN for a single-account AWS cloud connection.
// Updating IAM roles for organization-linked clouds is not allowed.
// Reconnect the cloud to change the IAM role.
func UpdateAwsCloudCredentials(ctx context.Context, apiClient *client.Client, id int64, input UpdateAwsCloudCredentialsInput) error {
	defer client.InvalidateCached(apiClient, cacheKey)
	return apiClient.Do(ctx, http.MethodPut, AWSBasePath+"/"+strconv.FormatInt(id, 10)+"/credentials", input, nil)
}
