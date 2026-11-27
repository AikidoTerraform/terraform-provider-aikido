package users

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

// The workspace roles the rights endpoint accepts.
const (
	RoleAdmin    = "admin"
	RoleDefault  = "default"
	RoleTeamOnly = "team_only"
)

// Capabilities are the permission flags the rights endpoint reads and writes,
// named as both the API field and the Terraform attribute. The deprecated
// can_ignore_or_snooze_issues is deliberately absent: the API derives it from
// can_ignore_issues and can_snooze_issues, so writing it back would grant both.
var Capabilities = []string{
	"can_ignore_issues",
	"can_snooze_issues",
	"can_change_issue_severity",
	"can_manage_teams",
	"can_manage_clouds",
	"can_manage_containers",
	"can_manage_domains",
	"can_manage_code_quality",
	"can_export_data",
	"can_manage_endpoint_protection",
	"can_manage_pentests",
	"can_manage_repos",
}

// teamOnlyCleared are the capabilities the team_only role forces off. The rest
// are left to the request, so a team-restricted user can still hold them.
var teamOnlyCleared = map[string]struct{}{
	"can_manage_teams":        {},
	"can_manage_clouds":       {},
	"can_manage_containers":   {},
	"can_manage_domains":      {},
	"can_manage_code_quality": {},
	"can_manage_pentests":     {},
}

// ForcedCapability reports the value a role fixes for a capability, ignoring
// whatever the request asked for. admin grants everything; team_only clears the
// resources it is not allowed to administer; default fixes nothing.
func ForcedCapability(role, capability string) (value bool, forced bool) {
	switch role {
	case RoleAdmin:
		return true, true
	case RoleTeamOnly:
		if _, cleared := teamOnlyCleared[capability]; cleared {
			return false, true
		}
	}

	return false, false
}

// ForcedReadOnly reports the read_only value a role fixes. Admins are never
// read-only.
func ForcedReadOnly(role string) (value bool, forced bool) {
	if role == RoleAdmin {
		return false, true
	}

	return false, false
}

// Effective applies a role's overrides to the requested capabilities, returning
// what Aikido will actually store. Sending these rather than the raw request
// keeps the write, the plan and the read-back in agreement.
func Effective(role string, requested map[string]bool) map[string]bool {
	effective := make(map[string]bool, len(Capabilities))
	for _, capability := range Capabilities {
		if forcedValue, forced := ForcedCapability(role, capability); forced {
			effective[capability] = forcedValue
			continue
		}
		effective[capability] = requested[capability]
	}

	return effective
}

// Flag is a permission value. The API documents these as 0 or 1 integers, but
// they are MySQL tinyints reaching JSON through more than one path, so booleans
// and strings are accepted too rather than failing the read.
type Flag bool

func (f *Flag) UnmarshalJSON(data []byte) error {
	switch strings.Trim(string(data), `"`) {
	case "true", "1":
		*f = true
	case "false", "0", "", "null":
		*f = false
	default:
		return fmt.Errorf("users: cannot read %s as a 0 or 1 permission value", data)
	}

	return nil
}

// Details is one user as returned by the detail endpoint, which is the only
// endpoint reporting role, read_only and permissions together.
type Details struct {
	ID          int64           `json:"id"`
	FullName    string          `json:"full_name"`
	Email       string          `json:"email"`
	Active      Flag            `json:"active"`
	Role        string          `json:"role"`
	ReadOnly    Flag            `json:"read_only"`
	AuthType    string          `json:"auth_type"`
	Permissions map[string]Flag `json:"permissions"`
}

// DetailPath is the path of a single user.
func DetailPath(id int64) string {
	return BasePath + "/" + strconv.FormatInt(id, 10)
}

// RightsPath is the path the role and permissions are written to.
func RightsPath(id int64) string {
	return DetailPath(id) + "/rights"
}

// Detail reads one user's role and permissions. It is deliberately uncached:
// it is read straight after a write, and the list endpoint cannot answer it.
func Detail(ctx context.Context, apiClient *client.Client, id int64) (Details, error) {
	var details Details
	if err := apiClient.Do(ctx, http.MethodGet, DetailPath(id), nil, &details); err != nil {
		return Details{}, err
	}

	return details, nil
}

// UpdateRights sets a user's role, read-only flag and capabilities. Every
// capability is sent: one the request omits keeps its stored value, which would
// leave an admin's permissions behind after a demotion.
func UpdateRights(ctx context.Context, apiClient *client.Client, id int64, role string, readOnly bool, permissions map[string]bool) error {
	body := map[string]any{
		"role":        role,
		"read_only":   readOnly,
		"permissions": permissions,
	}
	if err := apiClient.Do(ctx, http.MethodPut, RightsPath(id), body, nil); err != nil {
		return err
	}
	InvalidateAll(apiClient)

	return nil
}

// InvalidateAll drops the cached user list, which carries the role a write
// here may have just changed.
func InvalidateAll(apiClient *client.Client) {
	client.InvalidateCached(apiClient, allCacheKey)
}
