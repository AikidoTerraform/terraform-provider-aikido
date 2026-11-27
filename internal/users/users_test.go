package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"golang.org/x/time/rate"
)

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

// listServer serves the user list and records the query of every request.
func listServer(t *testing.T, queries *[]url.Values, users ...User) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != BasePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		*queries = append(*queries, r.URL.Query())
		if err := json.NewEncoder(w).Encode(users); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestAll_SortsByIDAndCaches(t *testing.T) {
	var queries []url.Values
	srv := listServer(t, &queries,
		User{ID: 30, Email: "c@example.com"},
		User{ID: 10, Email: "a@example.com"},
		User{ID: 20, Email: "b@example.com"},
	)
	apiClient := testClient(srv)

	all, err := All(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	wantIDs := []int64{10, 20, 30}
	if len(all) != len(wantIDs) {
		t.Fatalf("got %d users, want %d", len(all), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if all[i].ID != wantID {
			t.Errorf("position %d = id %d, want %d", i, all[i].ID, wantID)
		}
	}

	if _, err := All(context.Background(), apiClient); err != nil {
		t.Fatalf("second All: %v", err)
	}
	if len(queries) != 1 {
		t.Errorf("list requested %d times, want 1: the list must be cached per client", len(queries))
	}
}

// Deactivated users are omitted by default. Terraform must still see them, or a
// deactivated member looks like a membership that no longer exists.
func TestAll_AsksForInactiveUsers(t *testing.T) {
	var queries []url.Values
	srv := listServer(t, &queries)

	if _, err := All(context.Background(), testClient(srv)); err != nil {
		t.Fatalf("All: %v", err)
	}
	if got := queries[0].Get("include_inactive"); got != "1" {
		t.Errorf("include_inactive = %q, want 1", got)
	}
}

func TestIsActive(t *testing.T) {
	if !(User{Active: 1}).IsActive() {
		t.Error("active 1 reported as inactive")
	}
	if (User{Active: 0}).IsActive() {
		t.Error("active 0 reported as active")
	}
}

func TestInTeam_FiltersAndCachesPerTeam(t *testing.T) {
	var queries []url.Values
	srv := listServer(t, &queries, User{ID: 1, Email: "a@example.com", Active: 1})
	apiClient := testClient(srv)

	for range 2 {
		if _, err := InTeam(context.Background(), apiClient, 7); err != nil {
			t.Fatalf("InTeam(7): %v", err)
		}
	}
	if _, err := InTeam(context.Background(), apiClient, 8); err != nil {
		t.Fatalf("InTeam(8): %v", err)
	}

	if len(queries) != 2 {
		t.Fatalf("%d requests, want 2: one per team, cached thereafter", len(queries))
	}
	if got := queries[0].Get("filter_team_id"); got != "7" {
		t.Errorf("filter_team_id = %q, want 7", got)
	}
	if got := queries[0].Get("include_inactive"); got != "1" {
		t.Errorf("include_inactive = %q, want 1", got)
	}
	if got := queries[1].Get("filter_team_id"); got != "8" {
		t.Errorf("filter_team_id = %q, want 8", got)
	}
}

// A membership write must drop only its own team's cache: dropping every team's
// would undo the request saving this cache exists for.
func TestInvalidateTeam_DropsOnlyThatTeam(t *testing.T) {
	var queries []url.Values
	srv := listServer(t, &queries, User{ID: 1, Email: "a@example.com", Active: 1})
	apiClient := testClient(srv)

	if _, err := InTeam(context.Background(), apiClient, 7); err != nil {
		t.Fatalf("InTeam(7): %v", err)
	}
	if _, err := InTeam(context.Background(), apiClient, 8); err != nil {
		t.Fatalf("InTeam(8): %v", err)
	}

	InvalidateTeam(apiClient, 7)

	if _, err := InTeam(context.Background(), apiClient, 7); err != nil {
		t.Fatalf("InTeam(7) after invalidate: %v", err)
	}
	if _, err := InTeam(context.Background(), apiClient, 8); err != nil {
		t.Fatalf("InTeam(8) after invalidate: %v", err)
	}

	if len(queries) != 3 {
		t.Errorf("%d requests, want 3: only team 7 should be refetched", len(queries))
	}
}
