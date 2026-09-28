// Package teams models Aikido teams and centralizes their API calls, so that
// the team and membership resources read them the same way and share a single
// cached list per client.
package teams

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const (
	BasePath = "/public/v1/teams"

	// TypeCodeRepository is the only responsibility type the update endpoint accepts.
	TypeCodeRepository      = "code_repository"
	TypeContainerRepository = "container_repository"
	TypeCloud               = "cloud"
	TypeDomain              = "domain"
	TypeZenApp              = "zen_app"

	KindRepo   = "repo"
	KindImage  = "image"
	KindCloud  = "cloud"
	KindDomain = "domain"
	KindZenApp = "zen_app"

	LimitationInclude = "include"
	LimitationExclude = "exclude"

	pageSize = 100 // the teams endpoint caps per_page at 100
	cacheKey = "teams"
)

// LinkedResource is one resource the link and unlink endpoints accept. Kind is
// the Terraform/import identifier (repo, cloud, image, domain, zen_app); the
// API body uses a different field name, and the team list uses a different type.
type LinkedResource struct {
	Kind string
	ID   int64
}

// PathLimitation is the optional include/exclude filter on a linked repository.
type PathLimitation struct {
	Type  string
	Paths []string
}

var linkedKinds = map[string]struct {
	bodyField          string
	responsibilityType string
}{
	KindRepo:   {"repo_id", TypeCodeRepository},
	KindCloud:  {"cloud_id", TypeCloud},
	KindImage:  {"image_id", TypeContainerRepository},
	KindDomain: {"domain_id", TypeDomain},
	KindZenApp: {"zen_app_id", TypeZenApp},
}

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
// resource type and any path limitations. The team update endpoint still accepts
// only TypeCodeRepository with no paths; linking a resource uses Link and Unlink.
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
		return Team{}, fmt.Errorf("%w: team %d", client.ErrNotInList, id)
	}

	return cached, nil
}

// Create adds a manual team and returns its new ID. Responsibilities need a
// separate Update: the create endpoint takes a name only.
func Create(ctx context.Context, apiClient *client.Client, name string) (int64, error) {
	// Deferred: a call that fails may still have changed the team.
	defer InvalidateCache(apiClient)

	var created struct {
		ID int64 `json:"id"`
	}
	if err := apiClient.Do(ctx, http.MethodPost, BasePath, map[string]string{"name": name}, &created); err != nil {
		return 0, err
	}

	return created.ID, nil
}

type responsibilityWrite struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Update renames a team and, when codeRepoIDs is non-nil, makes its linked code
// repositories exactly those. The API diffs code repositories only, leaving
// clouds, containers, domains and Zen apps alone, so an empty non-nil slice
// unlinks every repository but no other resource; nil omits the key entirely,
// which the API reads as "no change". The body is a map rather than a struct
// because a struct cannot distinguish an empty list from an absent one.
func Update(ctx context.Context, apiClient *client.Client, id int64, name string, codeRepoIDs *[]int64) error {
	// Deferred: a call that fails may still have changed the team.
	defer InvalidateCache(apiClient)

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

	return nil
}

// Link attaches one resource to a team. limitation is only valid for a
// repository; other kinds ignore it because the API has no field for them.
func Link(ctx context.Context, apiClient *client.Client, teamID int64, linked LinkedResource, limitation *PathLimitation) error {
	defer InvalidateCache(apiClient)

	body, err := linkBody(linked, limitation)
	if err != nil {
		return err
	}

	return apiClient.Do(ctx, http.MethodPost, DetailPath(teamID)+"/linkResource", body, nil)
}

// Unlink detaches one resource from a team.
func Unlink(ctx context.Context, apiClient *client.Client, teamID int64, linked LinkedResource) error {
	defer InvalidateCache(apiClient)

	body, err := linkBody(linked, nil)
	if err != nil {
		return err
	}

	return apiClient.Do(ctx, http.MethodPost, DetailPath(teamID)+"/unlinkResource", body, nil)
}

// UpdatePathLimitation replaces the path filter on a repository already linked
// to the team. An empty Paths slice clears the filter. The endpoint does not
// change which resources are linked.
func UpdatePathLimitation(ctx context.Context, apiClient *client.Client, teamID, repoID int64, limitation PathLimitation) error {
	defer InvalidateCache(apiClient)

	body := map[string]any{
		"repo_id": repoID,
		"repo_path_limitation": map[string]any{
			"limitation_type": limitation.Type,
			"paths":           limitation.Paths,
		},
	}

	return apiClient.Do(ctx, http.MethodPost, DetailPath(teamID)+"/updateRepoPathLimitation", body, nil)
}

func linkBody(linked LinkedResource, limitation *PathLimitation) (map[string]any, error) {
	kind, ok := linkedKinds[linked.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown team resource kind %q", linked.Kind)
	}

	body := map[string]any{kind.bodyField: linked.ID}
	if limitation != nil {
		body["repo_path_limitation"] = map[string]any{
			"limitation_type": limitation.Type,
			"paths":           limitation.Paths,
		}
	}

	return body, nil
}

// Delete removes a manual team. The API rejects imported teams with a 400.
func Delete(ctx context.Context, apiClient *client.Client, id int64) error {
	// Deferred: a delete that reports failure may still have removed the team.
	defer InvalidateCache(apiClient)

	if err := apiClient.Do(ctx, http.MethodDelete, DetailPath(id), nil, nil); err != nil {
		return err
	}

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

// PathLimited returns the code repositories the team is responsible for only
// under a path filter. The update endpoint has no field for those paths, so
// repository_ids records such a repository as if the team covered all of it.
// The filters themselves survive an update; this is a fidelity limit, not a
// destructive one.
func PathLimited(team Team) []Responsibility {
	var limited []Responsibility
	for _, responsibility := range team.Responsibilities {
		if responsibility.Type != TypeCodeRepository {
			continue
		}
		if len(responsibility.IncludedPaths) > 0 || len(responsibility.ExcludedPaths) > 0 {
			limited = append(limited, responsibility)
		}
	}

	return limited
}

// KnownKind reports whether kind is one of the identifiers Link and Unlink accept.
func KnownKind(kind string) bool {
	_, ok := linkedKinds[kind]
	return ok
}

// ResponsibilityType is the type string the team list uses for kind.
func ResponsibilityType(kind string) string {
	return linkedKinds[kind].responsibilityType
}

// FindResponsibility returns the matching linked resource on the team, if any.
func FindResponsibility(team Team, kind string, id int64) (Responsibility, bool) {
	wantType := ResponsibilityType(kind)
	for _, responsibility := range team.Responsibilities {
		if responsibility.Type == wantType && responsibility.ID == id {
			return responsibility, true
		}
	}

	return Responsibility{}, false
}

// PathLimitationOf maps a responsibility's include/exclude lists into the
// single limitation object the link API accepts. Include wins if both are set.
func PathLimitationOf(responsibility Responsibility) *PathLimitation {
	if len(responsibility.IncludedPaths) > 0 {
		return &PathLimitation{Type: LimitationInclude, Paths: responsibility.IncludedPaths}
	}
	if len(responsibility.ExcludedPaths) > 0 {
		return &PathLimitation{Type: LimitationExclude, Paths: responsibility.ExcludedPaths}
	}

	return nil
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
