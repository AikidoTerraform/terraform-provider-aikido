package clouds

import (
	"context"
	"net/http"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const (
	ProviderAzure = "azure"
	AzureBasePath = "/public/v1/clouds/azure"
	AzureOrgPath  = "/public/v1/clouds/azure/management-account"
)

// AzureDetails holds Azure-specific fields. On the public API they are flat on the
// cloud object; Cloud.UnmarshalJSON / MarshalJSON project them here.
type AzureDetails struct {
	ApplicationId       string             `json:"application_id"`
	DirectoryId         string             `json:"directory_id"`
	AzureEnvironment    string             `json:"azure_environment"`
	IsManagementAccount bool               `json:"is_management_account"`
	Organization        *CloudOrganization `json:"organization"`
}

// AzureManagementGroupIDs converts the API sync scope to management group IDs.
func AzureManagementGroupIDs(syncedUnitIDs, directoryID string) []string {
	groups := SplitCommaSeparated(syncedUnitIDs)
	// The API represents tenant root scope with the directory ID.
	if len(groups) == 1 && groups[0] == directoryID {
		return []string{}
	}
	return groups
}

type CreateAzureInput struct {
	ApplicationId    string `json:"application_id"`
	SubscriptionId   string `json:"subscription_id"`
	DirectoryId      string `json:"directory_id"`
	KeyValue         string `json:"key_value"`
	Name             string `json:"name"`
	Environment      string `json:"environment"`
	AzureEnvironment string `json:"azure_environment,omitempty"`
}

type CreateAzureOrganizationInput struct {
	Name                       string `json:"name"`
	ApplicationId              string `json:"application_id"`
	DirectoryId                string `json:"directory_id"`
	KeyValue                   string `json:"key_value"`
	ManagementSubscriptionId   string `json:"management_subscription_id"`
	Environment                string `json:"environment"`
	AzureEnvironment           string `json:"azure_environment,omitempty"`
	ManagementGroupIDs         string `json:"management_group_ids,omitempty"`
	ExcludedManagementGroupIDs string `json:"excluded_management_group_ids,omitempty"`
	ExcludedSubscriptionIDs    string `json:"excluded_subscription_ids,omitempty"`
	VMScanEnabled              bool   `json:"vm_scan_enabled,omitempty"`
	ACRScanEnabled             bool   `json:"acr_scan_enabled,omitempty"`
}

type UpdateAzureCloudCredentialsInput struct {
	ClientSecret string `json:"client_secret"`
}

// CreateAzure connects a single Azure account and returns its new ID.
func CreateAzure(ctx context.Context, apiClient *client.Client, input CreateAzureInput) (int64, error) {
	defer client.InvalidateCached(apiClient, cacheKey)

	var created struct {
		ID int64 `json:"id"`
	}

	if err := apiClient.Do(ctx, http.MethodPost, AzureBasePath, input, &created); err != nil {
		return 0, err
	}

	return created.ID, nil
}

// CreateAzureOrganization connects an Azure Organization via its management account
// and returns its new ID.
func CreateAzureOrganization(ctx context.Context, apiClient *client.Client, input CreateAzureOrganizationInput) (int64, error) {
	defer client.InvalidateCached(apiClient, cacheKey)

	var created struct {
		ID int64 `json:"cloud_id"`
	}

	if err := apiClient.Do(ctx, http.MethodPost, AzureOrgPath, input, &created); err != nil {
		return 0, err
	}

	return created.ID, nil
}

// UpdateAzureCloudCredentials rotates the client secret for an Azure connection.
func UpdateAzureCloudCredentials(ctx context.Context, apiClient *client.Client, id int64, input UpdateAzureCloudCredentialsInput) error {
	defer client.InvalidateCached(apiClient, cacheKey)
	return apiClient.Do(ctx, http.MethodPut, AzureBasePath+"/"+strconv.FormatInt(id, 10)+"/credentials", input, nil)
}
