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

// A lost response does not mean the add was refused, and adding someone who is
// already a member succeeds, so the write can be confirmed by reading it back.
func TestCreateMembership_ReconcilesAnAmbiguousAdd(t *testing.T) {
	t.Run("an add that landed despite the error succeeds", func(t *testing.T) {
		var member []users.User
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case teams.BasePath:
				_ = json.NewEncoder(w).Encode([]teams.Team{manualTeam})
			case users.BasePath:
				_ = json.NewEncoder(w).Encode(member)
			default:
				// The write commits, then the response is lost.
				member = []users.User{{ID: 456, Active: 1}}
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		t.Cleanup(srv.Close)

		res := &teamUserResource{client: testClient(srv)}
		state, diagnostics := res.createMembership(context.Background(), teamUserModel{
			TeamID: types.Int64Value(123),
			UserID: types.Int64Value(456),
		})

		if diagnostics.HasError() {
			t.Fatalf("got %v, want the membership to be recognised as created", diagnostics)
		}
		if state.ID != types.StringValue("123:456") {
			t.Errorf("ID = %v, want \"123:456\"", state.ID)
		}
	})

	t.Run("an add that did not land still fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case teams.BasePath:
				_ = json.NewEncoder(w).Encode([]teams.Team{manualTeam})
			case users.BasePath:
				_ = json.NewEncoder(w).Encode([]users.User{})
			default:
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"reason_phrase":"The selected user id does not exist"}`)
			}
		}))
		t.Cleanup(srv.Close)

		res := &teamUserResource{client: testClient(srv)}
		_, diagnostics := res.createMembership(context.Background(), teamUserModel{
			TeamID: types.Int64Value(123),
			UserID: types.Int64Value(456),
		})

		if !diagnostics.HasError() {
			t.Fatal("an add that never landed produced no error")
		}
		if !strings.Contains(diagnostics.Errors()[0].Detail(), "The selected user id does not exist") {
			t.Errorf("detail %q drops the API's reason", diagnostics.Errors()[0].Detail())
		}
	})
}

func TestDeleteMembership(t *testing.T) {
	t.Run("removes the member from a manual team", func(t *testing.T) {
		var calls []string
		srv := membershipAPIServer(t, &calls, manualTeam, users.User{ID: 456, Active: 1})

		res := &teamUserResource{client: testClient(srv)}
		if diagnostics := res.deleteMembership(context.Background(), 123, 456); diagnostics.HasError() {
			t.Fatalf("deleteMembership: %v", diagnostics)
		}
		if !slicesContains(calls, "POST "+teams.BasePath+"/123/removeUser") {
			t.Errorf("calls = %v, want the removeUser POST", calls)
		}
	})

	// Aikido rejects this write, so the request would fail anyway; refusing here
	// says why instead of surfacing a bare API error during a destroy.
	t.Run("an imported team is refused before the write", func(t *testing.T) {
		var calls []string
		srv := membershipAPIServer(t, &calls, teams.Team{ID: 123, Name: "Frontend developers", ExternalSource: "github"})

		res := &teamUserResource{client: testClient(srv)}
		diagnostics := res.deleteMembership(context.Background(), 123, 456)

		if !diagnostics.HasError() {
			t.Fatal("destroying a membership of an imported team produced no error")
		}
		for _, call := range calls {
			if strings.Contains(call, "removeUser") {
				t.Errorf("calls = %v, want no write", calls)
			}
		}
	})

	// A team deleted outside Terraform took its memberships with it, so there is
	// nothing left to remove and nothing to report.
	t.Run("a team that no longer exists is not an error", func(t *testing.T) {
		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			_ = json.NewEncoder(w).Encode([]teams.Team{})
		}))
		t.Cleanup(srv.Close)

		res := &teamUserResource{client: testClient(srv)}
		if diagnostics := res.deleteMembership(context.Background(), 123, 456); diagnostics.HasError() {
			t.Fatalf("got %v, want no error", diagnostics)
		}
		for _, call := range calls {
			if strings.Contains(call, "removeUser") {
				t.Errorf("calls = %v, want no write", calls)
			}
		}
	})

	// A failed lookup is not proof the team is gone: reporting success here would
	// leave the user on the team with nothing tracking it.
	t.Run("a failing team lookup is an error, not a silent success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		res := &teamUserResource{client: testClient(srv)}
		if diagnostics := res.deleteMembership(context.Background(), 123, 456); !diagnostics.HasError() {
			t.Fatal("a 404 from the team list must not be reported as a completed destroy")
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
