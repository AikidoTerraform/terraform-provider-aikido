// Package teams models Aikido teams and centralizes their API calls, so that
// the team and membership resources read them the same way and share a single
// cached list per client.
package teams

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const (
	BasePath = "/public/v1/teams"

	// TypeCodeRepository is the only responsibility type the update endpoint accepts.
	TypeCodeRepository = "code_repository"

	pageSize = 100 // the teams endpoint caps per_page at 100
	cacheKey = "teams"
)

// Team is a team as returned by the list endpoint. Teams with an ExternalSource
// are owned by the Git provider and are not editable through Aikido.
type Team struct {
	ID               int64            `json:"id"`
	Name             string           `json:"name"`
	Active           bool             `json:"active"`
	ExternalSource   string           `json:"external_source"`
	ExternalSourceID string           `json:"external_source_id"`
	Responsibilities []Responsibility `json:"responsibilities"`
}

// Responsibility is a resource a team is responsible for. Reads return every
// resource type and any path limitations; writes accept only TypeCodeRepository
// with no paths.
type Responsibility struct {
	ID            int64    `json:"id"`
	Type          string   `json:"type"`
	IncludedPaths []string `json:"included_paths"`
	ExcludedPaths []string `json:"excluded_paths"`
}

// DetailPath is the path of a single team.
func DetailPath(id int64) string {
	return BasePath + "/" + strconv.FormatInt(id, 10)
}

// All returns every team from the shared list cache, sorted by ID so Terraform
// sees a stable order across plans.
func All(ctx context.Context, apiClient *client.Client) ([]Team, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return nil, err
	}

	all := make([]Team, 0, len(byID))
	for _, team := range byID {
		all = append(all, team)
	}
	slices.SortFunc(all, func(left, right Team) int {
		return cmp.Compare(left.ID, right.ID)
	})

	return all, nil
}

// ByID looks up one team in the shared list cache. There is no detail endpoint,
// so a workspace of N teams costs one paginated list rather than N requests.
func ByID(ctx context.Context, apiClient *client.Client, id int64) (Team, error) {
	byID, err := cachedByID(ctx, apiClient)
	if err != nil {
		return Team{}, err
	}

	cached, ok := byID[id]
	if !ok {
		return Team{}, &client.APIError{
			StatusCode: http.StatusNotFound,
			Method:     http.MethodGet,
			Path:       DetailPath(id),
			Body:       "team not found",
		}
	}

	return cached, nil
}

// Create adds a manual team and returns its new ID. Responsibilities need a
// separate Update: the create endpoint takes a name only.
func Create(ctx context.Context, apiClient *client.Client, name string) (int64, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	if err := apiClient.Do(ctx, http.MethodPost, BasePath, map[string]string{"name": name}, &created); err != nil {
		return 0, err
	}
	InvalidateCache(apiClient)

	return created.ID, nil
}

type responsibilityWrite struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Update renames a team and, when codeRepoIDs is non-nil, replaces its
// responsibilities with exactly those code repositories. An empty non-nil slice
// unlinks every resource; nil omits the key, which the API reads as "no change".
// The body is a map rather than a struct because a struct cannot distinguish an
// empty list from an absent one.
func Update(ctx context.Context, apiClient *client.Client, id int64, name string, codeRepoIDs *[]int64) error {
	body := map[string]any{"name": name}

	if codeRepoIDs != nil {
		responsibilities := make([]responsibilityWrite, 0, len(*codeRepoIDs))
		for _, codeRepoID := range *codeRepoIDs {
			responsibilities = append(responsibilities, responsibilityWrite{ID: codeRepoID, Type: TypeCodeRepository})
		}
		body["responsibilities"] = responsibilities
	}

	if err := apiClient.Do(ctx, http.MethodPut, DetailPath(id), body, nil); err != nil {
		return err
	}
	InvalidateCache(apiClient)

	return nil
}

// Delete removes a manual team. The API rejects imported teams with a 400.
func Delete(ctx context.Context, apiClient *client.Client, id int64) error {
	if err := apiClient.Do(ctx, http.MethodDelete, DetailPath(id), nil, nil); err != nil {
		return err
	}
	InvalidateCache(apiClient)

	return nil
}

// InvalidateCache drops the cached list so the next read reflects a write.
func InvalidateCache(apiClient *client.Client) {
	client.InvalidateCached(apiClient, cacheKey)
}

// IsImported reports whether the team is owned by a Git provider.
func IsImported(team Team) bool {
	return team.ExternalSource != ""
}

// CodeRepoIDs returns the team's code repository responsibilities, sorted so
// Terraform sees a stable set.
func CodeRepoIDs(team Team) []int64 {
	ids := make([]int64, 0, len(team.Responsibilities))
	for _, responsibility := range team.Responsibilities {
		if responsibility.Type == TypeCodeRepository {
			ids = append(ids, responsibility.ID)
		}
	}
	slices.Sort(ids)

	return ids
}

// Unrepresentable returns the responsibilities a full replace would destroy:
// non-code resources, which the update endpoint cannot express, and code
// repositories carrying path limitations, which it has no field for.
func Unrepresentable(team Team) []Responsibility {
	var unrepresentable []Responsibility
	for _, responsibility := range team.Responsibilities {
		if responsibility.Type != TypeCodeRepository ||
			len(responsibility.IncludedPaths) > 0 || len(responsibility.ExcludedPaths) > 0 {
			unrepresentable = append(unrepresentable, responsibility)
		}
	}

	return unrepresentable
}

// cachedByID fetches every team once per client and keys them by ID.
func cachedByID(ctx context.Context, apiClient *client.Client) (map[int64]Team, error) {
	return client.LoadCached(apiClient, ctx, cacheKey, func(ctx context.Context) (map[int64]Team, error) {
		items, err := client.FetchAllPages[Team](ctx, apiClient, BasePath, pageSize, "")
		if err != nil {
			return nil, err
		}

		byID := make(map[int64]Team, len(items))
		for _, team := range items {
			byID[team.ID] = team
		}

		return byID, nil
	})
}
