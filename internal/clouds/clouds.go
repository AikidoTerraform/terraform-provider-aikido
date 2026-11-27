package clouds

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	BasePath        = "/public/v1/clouds"
	pageSize        = 100
	cacheKey        = "clouds"
	listQueryParams = "org_details=true"

	// Environment purpose values accepted by the clouds API.
	EnvironmentProduction  = "production"
	EnvironmentStaging     = "staging"
	EnvironmentDevelopment = "development"
	EnvironmentMixed       = "mixed"
)

var SupportedProviders = []string{ProviderAWS, ProviderAzure}

// SupportedEnvironments are the environment purpose values the API accepts.
var SupportedEnvironments = []string{
	EnvironmentProduction,
	EnvironmentStaging,
	EnvironmentDevelopment,
	EnvironmentMixed,
}

func IsSupported(provider string) bool {
	return slices.Contains(SupportedProviders, provider)
}

func IsValidEnvironment(environment string) bool {
	return slices.Contains(SupportedEnvironments, environment)
}

type Cloud struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Environment   string `json:"environment"` // one of SupportedEnvironments
	ExternalID    string `json:"external_id"`
	LastScannedAt int64  `json:"last_scanned_at"`

	AWS   *AWSDetails   `json:"-"`
	Azure *AzureDetails `json:"-"`
}

type CloudOrganization struct {
	OrganizationID       string `json:"organization_id"`
	SyncedUnitIDs        string `json:"synced_unit_ids"`
	ExcludedAccounts     string `json:"excluded_accounts"`
	ExcludedFolders      string `json:"excluded_folders"`
	ContainerScanEnabled bool   `json:"container_scan_enabled"`
	VMScanEnabled        bool   `json:"vm_scan_enabled"`
}

type UpdateCloudInput struct {
	Name        string `json:"name"`
	Environment string `json:"environment"` // one of SupportedEnvironments
}

type UpdateCloudOrganizationInput struct {
	SyncedUnitIDs    string `json:"synced_unit_ids"`
	ExcludedAccounts string `json:"excluded_accounts"`
	ExcludedFolders  string `json:"excluded_folders"`
}

// GetAllClouds returns every cloud from the shared list cache, sorted by ID.
func GetAllClouds(ctx context.Context, apiClient *client.Client) ([]Cloud, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return nil, err
	}

	all := make([]Cloud, 0, len(byID))
	for _, cloud := range byID {
		all = append(all, cloud)
	}

	slices.SortFunc(all, func(left, right Cloud) int {
		return cmp.Compare(left.ID, right.ID)
	})

	return all, nil
}

// GetAllCloudsBySupportedProvider is GetAllClouds filtered to SupportedProviders.
func GetAllCloudsBySupportedProvider(ctx context.Context, apiClient *client.Client) ([]Cloud, error) {
	all, err := GetAllClouds(ctx, apiClient)
	if err != nil {
		return nil, err
	}

	supported := make([]Cloud, 0, len(all))
	for _, cloud := range all {
		if IsSupported(cloud.Provider) {
			supported = append(supported, cloud)
		}
	}

	return supported, nil
}

// ByID looks up one cloud in the shared list cache.
func ByID(ctx context.Context, apiClient *client.Client, id int64) (Cloud, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return Cloud{}, err
	}

	cached, ok := byID[id]
	if !ok {
		return Cloud{}, fmt.Errorf("%w: cloud %d", client.ErrNotInList, id)
	}

	return cached, nil
}

// Get fetches one cloud by ID from the detail endpoint and refreshes that entry
// in the list cache when present.
func Get(ctx context.Context, apiClient *client.Client, id int64) (Cloud, error) {
	var cloud Cloud
	if err := apiClient.Do(ctx, http.MethodGet, DetailPath(id)+"?"+listQueryParams, nil, &cloud); err != nil {
		return Cloud{}, err
	}

	client.UpdateCached(apiClient, cacheKey, func(byID map[int64]Cloud) map[int64]Cloud {
		next := make(map[int64]Cloud, len(byID)+1)

		for id, existing := range byID {
			next[id] = existing
		}

		next[cloud.ID] = cloud
		return next
	})

	return cloud, nil
}

// Delete removes a cloud connection.
func Delete(ctx context.Context, apiClient *client.Client, id int64) error {
	defer client.InvalidateCached(apiClient, cacheKey)
	return apiClient.Do(ctx, http.MethodDelete, DetailPath(id), nil, nil)
}

// Update updates a cloud connection.
func Update(ctx context.Context, apiClient *client.Client, id int64, input UpdateCloudInput) error {
	defer client.InvalidateCached(apiClient, cacheKey)
	return apiClient.Do(ctx, http.MethodPut, DetailPath(id), input, nil)
}

// update cloud organization settings.
func UpdateOrganization(ctx context.Context, apiClient *client.Client, id int64, input UpdateCloudOrganizationInput) error {
	defer client.InvalidateCached(apiClient, cacheKey)
	return apiClient.Do(ctx, http.MethodPut, DetailPath(id)+"/organization", input, nil)
}

// DetailPath is the path of a single cloud.
func DetailPath(id int64) string {
	return BasePath + "/" + strconv.FormatInt(id, 10)
}

func cachedByID(ctx context.Context, apiClient *client.Client) (map[int64]Cloud, error) {
	return client.LoadCached(apiClient, ctx, cacheKey, func(ctx context.Context) (map[int64]Cloud, error) {
		items, err := client.FetchAllPages[Cloud](ctx, apiClient, BasePath, pageSize, listQueryParams)

		if err != nil {
			return nil, err
		}

		byID := make(map[int64]Cloud, len(items))
		for _, cloud := range items {
			byID[cloud.ID] = cloud
		}

		return byID, nil
	})
}

func (c *Cloud) UnmarshalJSON(data []byte) error {
	type shared Cloud
	var decoded shared

	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	next := Cloud(decoded)

	var details any
	switch next.Provider {
	case ProviderAWS:
		next.AWS = &AWSDetails{}
		details = next.AWS
	case ProviderAzure:
		next.Azure = &AzureDetails{}
		details = next.Azure
	}

	if details != nil {
		if err := json.Unmarshal(data, details); err != nil {
			return err
		}
	}

	*c = next
	return nil
}

func (c Cloud) MarshalJSON() ([]byte, error) {
	type shared Cloud
	sharedJSON, err := json.Marshal(shared(c))
	if err != nil {
		return nil, err
	}

	var details any
	switch c.Provider {
	case ProviderAWS:
		if c.AWS != nil {
			details = c.AWS
		}
	case ProviderAzure:
		if c.Azure != nil {
			details = c.Azure
		}
	}

	if details == nil {
		return sharedJSON, nil
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return nil, err
	}

	return mergeJSONObjects(sharedJSON, detailsJSON)
}

func mergeJSONObjects(base, extra []byte) ([]byte, error) {
	var left, right map[string]json.RawMessage

	if err := json.Unmarshal(base, &left); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(extra, &right); err != nil {
		return nil, err
	}

	for key, value := range right {
		left[key] = value
	}

	return json.Marshal(left)
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

func StringListValues(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}

	values := []string{}
	diagnostics := list.ElementsAs(ctx, &values, false)

	return values, diagnostics
}

func ParseCloudID(cloudID string) (int64, error) {
	id, err := strconv.ParseInt(cloudID, 10, 64)

	if err != nil {
		return 0, fmt.Errorf("invalid cloud id %q: %w", cloudID, err)
	}

	return id, nil
}
