package users

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForcedCapability(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		capability string
		wantValue  bool
		wantForced bool
	}{
		{"admin grants everything", RoleAdmin, "can_manage_teams", true, true},
		{"admin grants issue handling too", RoleAdmin, "can_ignore_issues", true, true},
		{"team_only clears what it cannot administer", RoleTeamOnly, "can_manage_clouds", false, true},
		{"team_only leaves issue handling alone", RoleTeamOnly, "can_ignore_issues", false, false},
		{"team_only leaves repos alone", RoleTeamOnly, "can_manage_repos", false, false},
		{"team_only leaves endpoint protection alone", RoleTeamOnly, "can_manage_endpoint_protection", false, false},
		{"default fixes nothing", RoleDefault, "can_manage_teams", false, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, forced := ForcedCapability(test.role, test.capability)
			if value != test.wantValue || forced != test.wantForced {
				t.Errorf("ForcedCapability(%q, %q) = (%t, %t), want (%t, %t)",
					test.role, test.capability, value, forced, test.wantValue, test.wantForced)
			}
		})
	}
}

func TestForcedReadOnly(t *testing.T) {
	if value, forced := ForcedReadOnly(RoleAdmin); value || !forced {
		t.Errorf("admin read_only = (%t, %t), want (false, true)", value, forced)
	}
	if _, forced := ForcedReadOnly(RoleDefault); forced {
		t.Error("default must not fix read_only")
	}
}

func TestEffective(t *testing.T) {
	t.Run("admin overrides every request", func(t *testing.T) {
		effective := Effective(RoleAdmin, map[string]bool{"can_manage_teams": false})

		if len(effective) != len(Capabilities) {
			t.Fatalf("got %d capabilities, want %d", len(effective), len(Capabilities))
		}
		for capability, value := range effective {
			if !value {
				t.Errorf("%s = false, want true for an admin", capability)
			}
		}
	})

	t.Run("team_only clears some and keeps others", func(t *testing.T) {
		effective := Effective(RoleTeamOnly, map[string]bool{
			"can_manage_teams":  true,
			"can_ignore_issues": true,
		})

		if effective["can_manage_teams"] {
			t.Error("can_manage_teams = true, want false for a team_only user")
		}
		if !effective["can_ignore_issues"] {
			t.Error("can_ignore_issues = false, want the requested true")
		}
	})

	t.Run("default passes the request through, filling omissions with false", func(t *testing.T) {
		effective := Effective(RoleDefault, map[string]bool{"can_export_data": true})

		if !effective["can_export_data"] {
			t.Error("can_export_data = false, want the requested true")
		}
		if effective["can_manage_teams"] {
			t.Error("an omitted capability must be false, not inherited")
		}
		if len(effective) != len(Capabilities) {
			t.Errorf("got %d capabilities, want all %d sent", len(effective), len(Capabilities))
		}
	})
}

func TestFlag_ReadsEveryShapeTheAPIMightSend(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{`1`, true},
		{`0`, false},
		{`true`, true},
		{`false`, false},
		{`"1"`, true},
		{`"0"`, false},
		{`null`, false},
	}

	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			var flag Flag
			if err := json.Unmarshal([]byte(test.raw), &flag); err != nil {
				t.Fatalf("unmarshalling %s: %v", test.raw, err)
			}
			if bool(flag) != test.want {
				t.Errorf("%s = %t, want %t", test.raw, bool(flag), test.want)
			}
		})
	}

	t.Run("anything else is an error rather than a silent false", func(t *testing.T) {
		var flag Flag
		if err := json.Unmarshal([]byte(`"perhaps"`), &flag); err == nil {
			t.Error("want an error for an unreadable permission value")
		}
	})
}

func TestDetail_ReadsRoleAndPermissions(t *testing.T) {
	var path string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.WriteString(w, `{
			"id": 42, "full_name": "Alice", "email": "alice@example.com",
			"active": 1, "role": "team_only", "read_only": 0, "auth_type": "saml",
			"permissions": {"can_ignore_issues": 1, "can_manage_teams": 0}
		}`)
	}))
	t.Cleanup(srv.Close)

	details, err := Detail(context.Background(), testClient(srv), 42)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	if path != BasePath+"/42" {
		t.Errorf("path = %s, want %s/42", path, BasePath)
	}
	if details.Role != RoleTeamOnly || bool(details.ReadOnly) {
		t.Errorf("role = %q, read_only = %t", details.Role, bool(details.ReadOnly))
	}
	if !bool(details.Permissions["can_ignore_issues"]) || bool(details.Permissions["can_manage_teams"]) {
		t.Errorf("permissions = %v", details.Permissions)
	}
}

func TestUpdateRights_SendsEveryCapability(t *testing.T) {
	var method, path string
	var body struct {
		Role        string          `json:"role"`
		ReadOnly    bool            `json:"read_only"`
		Permissions map[string]bool `json:"permissions"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	permissions := Effective(RoleDefault, map[string]bool{"can_export_data": true})
	if err := UpdateRights(context.Background(), testClient(srv), 42, RoleDefault, false, permissions); err != nil {
		t.Fatalf("UpdateRights: %v", err)
	}

	if method != http.MethodPut || path != BasePath+"/42/rights" {
		t.Errorf("%s %s, want PUT %s/42/rights", method, path, BasePath)
	}
	if body.Role != RoleDefault {
		t.Errorf("role = %q", body.Role)
	}
	// An omitted capability must be written as false, not left to be inherited.
	if len(body.Permissions) != len(Capabilities) {
		t.Errorf("sent %d capabilities, want all %d", len(body.Permissions), len(Capabilities))
	}
	if !body.Permissions["can_export_data"] || body.Permissions["can_manage_teams"] {
		t.Errorf("permissions = %v", body.Permissions)
	}
}
