package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/teams"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/users"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTeamUserID(t *testing.T) {
	if got := teamUserID(123, 456); got != "123:456" {
		t.Errorf("teamUserID = %q, want 123:456", got)
	}
}

func TestParseTeamUserID(t *testing.T) {
	t.Run("a composite id yields both halves", func(t *testing.T) {
		teamID, userID, err := parseTeamUserID("123:456")
		if err != nil {
			t.Fatalf("parseTeamUserID: %v", err)
		}
		if teamID != 123 || userID != 456 {
			t.Errorf("got %d, %d, want 123, 456", teamID, userID)
		}
	})

	// Import is the only way a malformed id reaches the provider, so the error
	// has to say what the expected shape is.
	t.Run("malformed ids are rejected with the expected format", func(t *testing.T) {
		for _, id := range []string{"", "123", "a:b", "123:", ":456", "1:2:3"} {
			t.Run(id, func(t *testing.T) {
				_, _, err := parseTeamUserID(id)
				if err == nil {
					t.Fatalf("parseTeamUserID(%q) returned no error", id)
				}
				if !strings.Contains(err.Error(), "team_id:user_id") {
					t.Errorf("error %q does not state the expected format", err)
				}
			})
		}
	})
}

// membershipAPIServer answers the team list, the membership list and the
// add/remove calls, recording every request.
func membershipAPIServer(t *testing.T, calls *[]string, team teams.Team, members ...users.User) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)

		switch r.URL.Path {
		case teams.BasePath:
			_ = json.NewEncoder(w).Encode([]teams.Team{team})
		case users.BasePath:
			_ = json.NewEncoder(w).Encode(members)
		default:
			_, _ = io.WriteString(w, `{"success":1}`)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

var manualTeam = teams.Team{ID: 123, Name: "Security"}

func TestCreateMembership(t *testing.T) {
	t.Run("adds the user to the team", func(t *testing.T) {
		var calls []string
		var gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)

			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{manualTeam})
				return
			}
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_, _ = io.WriteString(w, `{"success":1}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamUserResource{client: testClient(srv)}
		state, diagnostics := res.createMembership(context.Background(), teamUserModel{
			TeamID: types.Int64Value(123),
			UserID: types.Int64Value(456),
		})
		if diagnostics.HasError() {
			t.Fatalf("createMembership: %v", diagnostics)
		}

		if !strings.Contains(gotBody, `"user_id":456`) {
			t.Errorf("body = %s, want the user id", gotBody)
		}
		if !slicesContains(calls, "POST "+teams.BasePath+"/123/addUser") {
			t.Errorf("calls = %v, want the addUser POST", calls)
		}
		if state.ID != types.StringValue("123:456") {
			t.Errorf("ID = %v, want \"123:456\"", state.ID)
		}
	})

	t.Run("an imported team is rejected before the write", func(t *testing.T) {
		var calls []string
		srv := membershipAPIServer(t, &calls, teams.Team{ID: 123, Name: "Frontend developers", ExternalSource: "github"})

		res := &teamUserResource{client: testClient(srv)}
		_, diagnostics := res.createMembership(context.Background(), teamUserModel{
			TeamID: types.Int64Value(123),
			UserID: types.Int64Value(456),
		})

		if !diagnostics.HasError() {
			t.Fatal("membership on an imported team produced no error")
		}
		for _, call := range calls {
			if strings.Contains(call, "addUser") {
				t.Errorf("calls = %v, want no write", calls)
			}
		}
	})

	// The API answers 404 for both an unknown team and an unknown user, so the
	// message must carry the API's reason and say which two things to check.
	t.Run("a rejected add explains what the API refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{manualTeam})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"reason_phrase":"user not found"}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamUserResource{client: testClient(srv)}
		_, diagnostics := res.createMembership(context.Background(), teamUserModel{
			TeamID: types.Int64Value(123),
			UserID: types.Int64Value(456),
		})

		if !diagnostics.HasError() {
			t.Fatal("a 404 from addUser produced no error")
		}
		detail := diagnostics.Errors()[0].Detail()
		if !strings.Contains(detail, "user not found") {
			t.Errorf("detail %q drops the API's reason", detail)
		}
	})
}

func TestReadMembership(t *testing.T) {
	t.Run("a member is found", func(t *testing.T) {
		var calls []string
		srv := membershipAPIServer(t, &calls, manualTeam, users.User{ID: 456, Active: 1})

		res := &teamUserResource{client: testClient(srv)}
		found, diagnostics := res.readMembership(context.Background(), 123, 456)
		if diagnostics.HasError() {
			t.Fatalf("readMembership: %v", diagnostics)
		}
		if !found {
			t.Error("found = false, want true")
		}
	})

	t.Run("a non-member is not found", func(t *testing.T) {
		var calls []string
		srv := membershipAPIServer(t, &calls, manualTeam, users.User{ID: 999, Active: 1})

		res := &teamUserResource{client: testClient(srv)}
		found, diagnostics := res.readMembership(context.Background(), 123, 456)
		if diagnostics.HasError() {
			t.Fatalf("readMembership: %v", diagnostics)
		}
		if found {
			t.Error("found = true, want false")
		}
	})

	// Deactivating someone does not remove them from the team. Treating them as
	// absent would churn a remove and re-add on every plan.
	t.Run("a deactivated member is still a member", func(t *testing.T) {
		var calls []string
		srv := membershipAPIServer(t, &calls, manualTeam, users.User{ID: 456, Active: 0})

		res := &teamUserResource{client: testClient(srv)}
		found, diagnostics := res.readMembership(context.Background(), 123, 456)
		if diagnostics.HasError() {
			t.Fatalf("readMembership: %v", diagnostics)
		}
		if !found {
			t.Error("found = false, want true for a deactivated member")
		}
	})
}

func TestRemoveMembership(t *testing.T) {
	var calls []string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	res := &teamUserResource{client: testClient(srv)}
	if err := res.removeMember(context.Background(), 123, 456); err != nil {
		t.Fatalf("removeMember: %v", err)
	}

	if !slicesContains(calls, "POST "+teams.BasePath+"/123/removeUser") {
		t.Errorf("calls = %v, want the removeUser POST", calls)
	}
	if !strings.Contains(gotBody, `"user_id":456`) {
		t.Errorf("body = %s, want the user id", gotBody)
	}
}

// Membership writes must drop the team's cached member list, or a Read later in
// the same apply serves the pre-write membership back to Terraform.
func TestCreateMembership_InvalidatesTheTeamMemberCache(t *testing.T) {
	var memberListCount int
	var member users.User
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case teams.BasePath:
			_ = json.NewEncoder(w).Encode([]teams.Team{manualTeam})
		case users.BasePath:
			memberListCount++
			_ = json.NewEncoder(w).Encode([]users.User{member})
		default:
			member = users.User{ID: 456, Active: 1}
			_, _ = io.WriteString(w, `{"success":1}`)
		}
	}))
	t.Cleanup(srv.Close)

	res := &teamUserResource{client: testClient(srv)}

	// Prime the cache with a team that has no members yet.
	if _, diagnostics := res.readMembership(context.Background(), 123, 456); diagnostics.HasError() {
		t.Fatalf("readMembership: %v", diagnostics)
	}

	if _, diagnostics := res.createMembership(context.Background(), teamUserModel{
		TeamID: types.Int64Value(123),
		UserID: types.Int64Value(456),
	}); diagnostics.HasError() {
		t.Fatalf("createMembership: %v", diagnostics)
	}

	found, diagnostics := res.readMembership(context.Background(), 123, 456)
	if diagnostics.HasError() {
		t.Fatalf("readMembership after add: %v", diagnostics)
	}
	if !found {
		t.Error("found = false: the add must invalidate the cached member list")
	}
	if memberListCount != 2 {
		t.Errorf("member list requested %d times, want 2", memberListCount)
	}
}

func slicesContains(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}

	return false
}
