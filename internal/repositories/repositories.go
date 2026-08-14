// Package repositories models Aikido code repositories and centralizes the
// list/detail API calls, so that both managed resources and data sources read
// them the same way and share a single cached list per client.
package repositories

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const (
	BasePath = "/public/v1/repositories/code"

	pageSize = 200
	cacheKey = "repositories/code"

	listQueryParams = "include_inactive=true&include_labels=true"
)

// Repository is a code repository as returned by the list and detail endpoints.
// Fields only present on the detail endpoint (linked_teams, excluded_paths) are
// deliberately not modeled: reading them would cost one request per repository
// and defeat the shared list cache.
type Repository struct {
	ID                    int64   `json:"id"`
	Name                  string  `json:"name"`
	Provider              string  `json:"provider"`
	ExternalRepoID        string  `json:"external_repo_id"`
	ExternalRepoNumericID int64   `json:"external_repo_numeric_id"`
	Active                bool    `json:"active"`
	Branch                string  `json:"branch"`
	URL                   string  `json:"url"`
	LastScannedAt         int64   `json:"last_scanned_at"`
	Connectivity          string  `json:"connectivity"`
	Sensitivity           string  `json:"sensitivity"`
	Labels                []Label `json:"labels"`
}

type Label struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsImported bool   `json:"is_imported"`
}

// DetailPath is the detail endpoint for a single repository.
func DetailPath(id int64) string {
	return BasePath + "/" + strconv.FormatInt(id, 10)
}

// ByID looks up one repository in the shared paginated list cache. Use for Read
// when many resources share one plan, so a workspace of N repos costs one
// paginated list rather than N detail GETs.
func ByID(ctx context.Context, apiClient *client.Client, id int64) (Repository, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return Repository{}, err
	}

	cached, ok := byID[id]
	if !ok {
		return Repository{}, &client.APIError{
			StatusCode: http.StatusNotFound,
			Method:     http.MethodGet,
			Path:       DetailPath(id),
			Body:       "repository not found",
		}
	}

	return cached, nil
}

// All returns every repository from the shared list cache, sorted by ID so
// Terraform sees a stable order across plans.
func All(ctx context.Context, apiClient *client.Client) ([]Repository, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return nil, err
	}

	all := make([]Repository, 0, len(byID))
	for _, repo := range byID {
		all = append(all, repo)
	}
	slices.SortFunc(all, func(left, right Repository) int {
		return cmp.Compare(left.ID, right.ID)
	})

	return all, nil
}

// Detail loads one repository via GET /repositories/code/{id}. Use after writes
// so state reflects the API rather than a possibly stale list cache.
func Detail(ctx context.Context, apiClient *client.Client, id int64) (Repository, error) {
	var repo Repository
	if err := apiClient.Do(ctx, http.MethodGet, DetailPath(id), nil, &repo); err != nil {
		return Repository{}, err
	}

	return repo, nil
}

// cachedByID fetches every repository once per client and keys them by ID.
func cachedByID(ctx context.Context, apiClient *client.Client) (map[int64]Repository, error) {
	return client.LoadCached(apiClient, ctx, cacheKey, func(ctx context.Context) (map[int64]Repository, error) {
		items, err := client.FetchAllPages[Repository](ctx, apiClient, BasePath, pageSize, listQueryParams)
		if err != nil {
			return nil, err
		}

		byID := make(map[int64]Repository, len(items))
		for _, repo := range items {
			byID[repo.ID] = repo
		}

		return byID, nil
	})
}
