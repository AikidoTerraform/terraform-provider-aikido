package datasources

import (
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/users"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestUserMatchesFilters(t *testing.T) {
	user := users.User{
		ID:       7,
		FullName: "Wendy Snutz",
		Email:    "wendy@example.com",
		Active:   1,
		Role:     "admin",
		AuthType: "github",
	}

	tests := []struct {
		name   string
		config usersDataSourceModel
		want   bool
	}{
		{
			name:   "no filters matches everything",
			config: usersDataSourceModel{},
			want:   true,
		},
		{
			name:   "email matches exactly",
			config: usersDataSourceModel{Email: types.StringValue("wendy@example.com")},
			want:   true,
		},
		// Identity providers disagree about case, and the same account can come
		// back capitalised differently than it was written in the configuration.
		{
			name:   "email matching ignores case",
			config: usersDataSourceModel{Email: types.StringValue("Wendy@Example.com")},
			want:   true,
		},
		{
			name:   "email is not a substring match",
			config: usersDataSourceModel{Email: types.StringValue("wendy")},
			want:   false,
		},
		{
			name:   "full name matches exactly",
			config: usersDataSourceModel{FullName: types.StringValue("Wendy Snutz")},
			want:   true,
		},
		{
			name:   "full name mismatch excludes",
			config: usersDataSourceModel{FullName: types.StringValue("Wendy")},
			want:   false,
		},
		{
			name:   "role matches",
			config: usersDataSourceModel{Role: types.StringValue("admin")},
			want:   true,
		},
		{
			name:   "role mismatch excludes",
			config: usersDataSourceModel{Role: types.StringValue("team_only")},
			want:   false,
		},
		{
			name:   "auth type matches",
			config: usersDataSourceModel{AuthType: types.StringValue("github")},
			want:   true,
		},
		{
			name:   "id matches",
			config: usersDataSourceModel{ID: types.Int64Value(7)},
			want:   true,
		},
		{
			name:   "id mismatch excludes",
			config: usersDataSourceModel{ID: types.Int64Value(8)},
			want:   false,
		},
		{
			name:   "active true keeps an active user",
			config: usersDataSourceModel{Active: types.BoolValue(true)},
			want:   true,
		},
		{
			name:   "active false excludes an active user",
			config: usersDataSourceModel{Active: types.BoolValue(false)},
			want:   false,
		},
		{
			name: "filters combine with AND",
			config: usersDataSourceModel{
				Email: types.StringValue("wendy@example.com"),
				Role:  types.StringValue("team_only"),
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := userMatchesFilters(user, test.config); got != test.want {
				t.Errorf("userMatchesFilters = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUserMatchesFilters_InactiveUser(t *testing.T) {
	user := users.User{ID: 7, Active: 0}

	if userMatchesFilters(user, usersDataSourceModel{Active: types.BoolValue(true)}) {
		t.Error("inactive user matched active = true")
	}
	if !userMatchesFilters(user, usersDataSourceModel{Active: types.BoolValue(false)}) {
		t.Error("inactive user did not match active = false")
	}
	if !userMatchesFilters(user, usersDataSourceModel{}) {
		t.Error("inactive user excluded by an unfiltered lookup")
	}
}

func TestMatchingUsers_IDsStayAlignedAfterFiltering(t *testing.T) {
	workspace := []users.User{
		{ID: 10, Email: "a@example.com", Active: 1, Role: "admin"},
		{ID: 20, Email: "b@example.com", Active: 0, Role: "default"},
		{ID: 30, Email: "c@example.com", Active: 1, Role: "default"},
	}

	tests := []struct {
		name    string
		config  usersDataSourceModel
		wantIDs []int64
	}{
		{
			name:    "no filters returns everyone",
			config:  usersDataSourceModel{},
			wantIDs: []int64{10, 20, 30},
		},
		{
			name:    "role filter",
			config:  usersDataSourceModel{Role: types.StringValue("default")},
			wantIDs: []int64{20, 30},
		},
		{
			name:    "active filter",
			config:  usersDataSourceModel{Active: types.BoolValue(true)},
			wantIDs: []int64{10, 30},
		},
		{
			name:    "no match yields empty, not null",
			config:  usersDataSourceModel{Email: types.StringValue("nobody@example.com")},
			wantIDs: []int64{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matched, matchedIDs := matchingUsers(workspace, test.config)

			if matched == nil || matchedIDs == nil {
				t.Fatal("matchingUsers returned nil; want non-nil slices so Terraform sees an empty list")
			}
			if len(matched) != len(matchedIDs) {
				t.Fatalf("%d users but %d ids", len(matched), len(matchedIDs))
			}
			if len(matchedIDs) != len(test.wantIDs) {
				t.Fatalf("got %d matches, want %d", len(matchedIDs), len(test.wantIDs))
			}

			for i, wantID := range test.wantIDs {
				if matchedIDs[i].ValueInt64() != wantID {
					t.Errorf("ids[%d] = %d, want %d", i, matchedIDs[i].ValueInt64(), wantID)
				}
				if matched[i].ID.ValueInt64() != wantID {
					t.Errorf("users[%d].id = %d, want %d", i, matched[i].ID.ValueInt64(), wantID)
				}
			}
		})
	}
}

func TestUserModelFromAPI_ActiveBecomesBool(t *testing.T) {
	model := userModelFromAPI(users.User{
		ID:                 7,
		FullName:           "Wendy Snutz",
		Email:              "wendy@example.com",
		Active:             1,
		LastLoginTimestamp: 1720059659,
		Role:               "admin",
		AuthType:           "github",
	})

	if model.Active != types.BoolValue(true) {
		t.Errorf("Active = %v, want true: the API's 0 or 1 must not surface as a number", model.Active)
	}
	if model.ID != types.Int64Value(7) {
		t.Errorf("ID = %v, want 7", model.ID)
	}
	if model.LastLoginTimestamp != types.Int64Value(1720059659) {
		t.Errorf("LastLoginTimestamp = %v", model.LastLoginTimestamp)
	}
}

func TestUnknownUserFilterDiagnostics(t *testing.T) {
	t.Run("known filters produce no error", func(t *testing.T) {
		config := usersDataSourceModel{Email: types.StringValue("wendy@example.com")}

		if diagnostics := unknownUserFilterDiagnostics(config); diagnostics.HasError() {
			t.Errorf("got %v, want no diagnostics", diagnostics)
		}
	})

	// An ignored filter would return the whole workspace, and that result feeds
	// user_id on membership resources.
	t.Run("each unknown filter is reported", func(t *testing.T) {
		tests := []struct {
			name   string
			config usersDataSourceModel
		}{
			{"email", usersDataSourceModel{Email: types.StringUnknown()}},
			{"full_name", usersDataSourceModel{FullName: types.StringUnknown()}},
			{"role", usersDataSourceModel{Role: types.StringUnknown()}},
			{"auth_type", usersDataSourceModel{AuthType: types.StringUnknown()}},
			{"active", usersDataSourceModel{Active: types.BoolUnknown()}},
			{"id", usersDataSourceModel{ID: types.Int64Unknown()}},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				diagnostics := unknownUserFilterDiagnostics(test.config)
				if !diagnostics.HasError() {
					t.Fatalf("unknown %s produced no error", test.name)
				}
				if !strings.Contains(diagnostics.Errors()[0].Detail(), test.name) {
					t.Errorf("error detail %q does not name the %s filter",
						diagnostics.Errors()[0].Detail(), test.name)
				}
			})
		}
	})
}
