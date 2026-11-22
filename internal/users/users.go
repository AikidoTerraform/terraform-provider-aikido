// Package users models Aikido workspace users and centralizes their API calls,
// so that the users data source and the team membership resource read them the
// same way and share cached lists per client.
package users

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const (
	BasePath = "/public/v1/users"

	// The endpoint omits deactivated users unless asked. Terraform needs them:
	// without this, a deactivated member reads as a membership that is gone.
	includeInactive = "include_inactive=1"

	allCacheKey = "users"
)

// User is a workspace user as returned by the list endpoint. Active is the API's
// 0 or 1 integer rather than a boolean; use IsActive.
type User struct {
	ID                 int64  `json:"id"`
	FullName           string `json:"full_name"`
	Email              string `json:"email"`
	Active             int64  `json:"active"`
	LastLoginTimestamp int64  `json:"last_login_timestamp"`
	Role               string `json:"role"`
	AuthType           string `json:"auth_type"`
}

// IsActive reports whether the user is active in the workspace.
func (u User) IsActive() bool {
	return u.Active == 1
}

// All returns every user in the workspace, sorted by ID so Terraform sees a
// stable order across plans. The endpoint has no pagination, so this is one
// request covering the whole workspace.
func All(ctx context.Context, apiClient *client.Client) ([]User, error) {
	return client.LoadCached(apiClient, ctx, allCacheKey, func(ctx context.Context) ([]User, error) {
		return list(ctx, apiClient, "")
	})
}

// InTeam returns the members of one team. The result is cached per team, so N
// membership resources spread over M teams cost M requests rather than N.
func InTeam(ctx context.Context, apiClient *client.Client, teamID int64) ([]User, error) {
	return client.LoadCached(apiClient, ctx, teamCacheKey(teamID), func(ctx context.Context) ([]User, error) {
		return list(ctx, apiClient, "&filter_team_id="+strconv.FormatInt(teamID, 10))
	})
}

// InvalidateTeam drops one team's cached membership so the next read reflects a
// write. Other teams keep their cache.
func InvalidateTeam(apiClient *client.Client, teamID int64) {
	client.InvalidateCached(apiClient, teamCacheKey(teamID))
}

func teamCacheKey(teamID int64) string {
	return "users/team/" + strconv.FormatInt(teamID, 10)
}

func list(ctx context.Context, apiClient *client.Client, extraQueryParams string) ([]User, error) {
	var listed []User
	if err := apiClient.Do(ctx, http.MethodGet, BasePath+"?"+includeInactive+extraQueryParams, nil, &listed); err != nil {
		return nil, err
	}

	slices.SortFunc(listed, func(left, right User) int {
		return cmp.Compare(left.ID, right.ID)
	})

	return listed, nil
}
