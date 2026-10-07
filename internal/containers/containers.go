// Package containers models Aikido container repositories and centralizes the
// list/detail API calls, so that both managed resources and data sources read
// them the same way and share a single cached list per client.
package containers

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/labels"
)

const (
	BasePath = "/public/v1/containers"

	pageSize = 100
	cacheKey = "containers"

	// filter_status defaults to active, and labels, sensitivity and connectivity
	// are opt-in. All four are needed, or managed attributes read back null.
	listQueryParams = "filter_status=all&include_labels=true" +
		"&include_sensitivity=true&include_connectivity=true"
)

type Label = labels.Label

// Container is a container repository as returned by the list and detail
// endpoints. The list-only fields last_pushed_at, created_at, is_empty,
// is_running and exposed_via are deliberately not modeled: nothing exposes them,
// and a modeled field reads as a supported attribute.
//
// TagFilter maps the tag field, which the API documents as the filter used when
// selecting an image rather than the tag that was scanned. The scanned tag is
// LastScannedTag.
type Container struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Provider          string  `json:"provider"`
	CloudID           *int64  `json:"cloud_id"`
	RegistryID        *int64  `json:"registry_id"`
	RegistryName      string  `json:"registry_name"`
	TagFilter         string  `json:"tag"`
	Distro            string  `json:"distro"`
	DistroVersion     string  `json:"distro_version"`
	LastScannedAt     int64   `json:"last_scanned_at"`
	LastScannedTag    string  `json:"last_scanned_tag"`
	LastScannedDigest string  `json:"last_scanned_digest"`
	LinkedCodeRepoID  *int64  `json:"-"`
	Active            bool    `json:"is_active"`
	Connectivity      string  `json:"connectivity"`
	Sensitivity       string  `json:"sensitivity"`
	Labels            []Label `json:"labels"`
}

// UnmarshalJSON accepts linked_code_repo_id as a JSON number, a quoted number or
// null. The list endpoint documents an integer and the detail endpoint a string,
// and a whole page decodes in one call, so any shape this type rejects fails
// every container read rather than a single field.
func (c *Container) UnmarshalJSON(data []byte) error {
	// plain drops the UnmarshalJSON method, so decoding it does not recurse.
	type plain Container

	var raw struct {
		plain
		LinkedCodeRepoID json.RawMessage `json:"linked_code_repo_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*c = Container(raw.plain)

	id, err := optionalInt64(raw.LinkedCodeRepoID)
	if err != nil {
		return fmt.Errorf("linked_code_repo_id: %w", err)
	}
	// A container with no linked code repository reports a non-positive sentinel
	// rather than null, and a sentinel read as an ID would name a repository the
	// container is not linked to.
	if id != nil && *id <= 0 {
		id = nil
	}
	c.LinkedCodeRepoID = id

	return nil
}

// DetailPath is the detail endpoint for a single container.
func DetailPath(id int64) string {
	return BasePath + "/" + strconv.FormatInt(id, 10)
}

// ByID looks up one container in the shared paginated list cache. Use for Read
// when many resources share one plan, so a workspace of N containers costs one
// paginated list rather than N detail GETs.
func ByID(ctx context.Context, apiClient *client.Client, id int64) (Container, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return Container{}, err
	}

	cached, ok := byID[id]
	if !ok {
		return Container{}, fmt.Errorf("%w: container %d", client.ErrNotInList, id)
	}

	return cached, nil
}

// All returns every container from the shared list cache, sorted by ID so
// Terraform sees a stable order across plans.
func All(ctx context.Context, apiClient *client.Client) ([]Container, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return nil, err
	}

	all := make([]Container, 0, len(byID))
	for _, container := range byID {
		all = append(all, container)
	}
	slices.SortFunc(all, func(left, right Container) int {
		return cmp.Compare(left.ID, right.ID)
	})

	return all, nil
}

// Detail loads one container via GET /containers/{id}. Use after writes so state
// reflects the API rather than a possibly stale list cache. The detail endpoint is
// not documented to return sensitivity or connectivity; callers compose those.
func Detail(ctx context.Context, apiClient *client.Client, id int64) (Container, error) {
	var container Container
	if err := apiClient.Do(ctx, http.MethodGet, DetailPath(id), nil, &container); err != nil {
		return Container{}, err
	}

	return container, nil
}

// InvalidateCache drops the cached list so the next read reflects a write.
// Callers that mutate a container must invoke it, otherwise a data source reading
// later in the same apply still sees the pre-write list.
func InvalidateCache(apiClient *client.Client) {
	client.InvalidateCached(apiClient, cacheKey)
}

// cachedByID fetches every container once per client and keys them by ID.
func cachedByID(ctx context.Context, apiClient *client.Client) (map[int64]Container, error) {
	return client.LoadCached(apiClient, ctx, cacheKey, func(ctx context.Context) (map[int64]Container, error) {
		items, err := client.FetchAllPages[Container](ctx, apiClient, BasePath, pageSize, listQueryParams)
		if err != nil {
			return nil, err
		}

		byID := make(map[int64]Container, len(items))
		for _, container := range items {
			byID[container.ID] = container
		}

		return byID, nil
	})
}

// optionalInt64 decodes an absent, null, numeric or quoted-numeric value.
func optionalInt64(raw json.RawMessage) (*int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		if text == "" {
			return nil, nil
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, err
		}

		return &parsed, nil
	}

	var number int64
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, err
	}

	return &number, nil
}
