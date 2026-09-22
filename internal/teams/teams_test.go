package teams

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"golang.org/x/time/rate"
)

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

// listServer serves one page of teams and counts how often it is called.
func listServer(t *testing.T, requestCount *int, teams ...Team) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != BasePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		*requestCount++
		if err := json.NewEncoder(w).Encode(teams); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestAll_SortsByIDAndCaches(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount,
		Team{ID: 30, Name: "gamma"},
		Team{ID: 10, Name: "alpha"},
		Team{ID: 20, Name: "beta"},
	)
	apiClient := testClient(srv)

	all, err := All(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	wantIDs := []int64{10, 20, 30}
	if len(all) != len(wantIDs) {
		t.Fatalf("got %d teams, want %d", len(all), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if all[i].ID != wantID {
			t.Errorf("position %d = id %d, want %d", i, all[i].ID, wantID)
		}
	}

	if _, err := All(context.Background(), apiClient); err != nil {
		t.Fatalf("second All: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("list requested %d times, want 1: the list must be cached per client", requestCount)
	}
}

// per_page is capped at 100 on this endpoint, unlike the repositories list.
func TestAll_RequestsTheMaximumSupportedPageSize(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if err := json.NewEncoder(w).Encode([]Team{}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	if _, err := All(context.Background(), testClient(srv)); err != nil {
		t.Fatalf("All: %v", err)
	}
	if !strings.Contains(gotQuery, "per_page=100") {
		t.Errorf("query = %q, want per_page=100", gotQuery)
	}
}

func TestByID(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Team{ID: 7, Name: "Security"})
	apiClient := testClient(srv)

	team, err := ByID(context.Background(), apiClient, 7)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if team.Name != "Security" {
		t.Errorf("Name = %q, want Security", team.Name)
	}

	_, err = ByID(context.Background(), apiClient, 8)
	if err == nil {
		t.Fatal("ByID for an absent team returned no error")
	}
	if !client.NotInList(err) {
		t.Errorf("error = %v, want one client.NotInList reports so Read can drop the resource", err)
	}
	if client.NotFound(err) {
		t.Errorf("error = %v, must not also read as an API 404", err)
	}
}

// A 404 from the list request means the lookup failed, so the team must not be
// dropped from state.
func TestByID_FailedListIsNotAMissingTeam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := ByID(context.Background(), testClient(srv), 1)
	if err == nil {
		t.Fatal("want an error when the list request fails")
	}
	if client.NotInList(err) {
		t.Errorf("error = %v, must not read as a team that is gone", err)
	}
}

func TestCreate_PostsNameAndReturnsID(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != BasePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)

		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]int64{"id": 42}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	id, err := Create(context.Background(), testClient(srv), "Security")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	if !strings.Contains(gotBody, `"name":"Security"`) {
		t.Errorf("body = %s, want the team name", gotBody)
	}
}

// The API replaces the responsibility set wholesale, so nil (leave alone) and an
// empty slice (unlink everything) must produce different request bodies.
func TestUpdate_ResponsibilitiesPayload(t *testing.T) {
	tests := []struct {
		name        string
		codeRepoIDs *[]int64
		wantBody    string
		wantAbsent  bool
	}{
		{
			name:        "ids are sent as code_repository responsibilities",
			codeRepoIDs: &[]int64{1, 2},
			wantBody:    `"responsibilities":[{"id":1,"type":"code_repository"},{"id":2,"type":"code_repository"}]`,
		},
		{
			name:        "an empty set unlinks every repository",
			codeRepoIDs: &[]int64{},
			wantBody:    `"responsibilities":[]`,
		},
		{
			name:        "nil omits the key so responsibilities are left untouched",
			codeRepoIDs: nil,
			wantAbsent:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != BasePath+"/5" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				gotBody = string(body)

				if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
					t.Errorf("encoding response: %v", err)
				}
			}))
			t.Cleanup(srv.Close)

			if err := Update(context.Background(), testClient(srv), 5, "Security", test.codeRepoIDs); err != nil {
				t.Fatalf("Update: %v", err)
			}

			if !strings.Contains(gotBody, `"name":"Security"`) {
				t.Errorf("body = %s, want the team name", gotBody)
			}
			// null and an absent key both decode to nil, so assert on the raw body.
			if test.wantAbsent {
				if strings.Contains(gotBody, "responsibilities") {
					t.Errorf("body = %s, want no responsibilities key", gotBody)
				}
				return
			}
			if !strings.Contains(gotBody, test.wantBody) {
				t.Errorf("body = %s, want %s", gotBody, test.wantBody)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	if err := Delete(context.Background(), testClient(srv), 5); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != BasePath+"/5" {
		t.Errorf("got %s %s, want DELETE %s/5", gotMethod, gotPath, BasePath)
	}
}

// Writes must drop the cached list, or a Read later in the same apply serves the
// pre-write state back to Terraform.
func TestWritesInvalidateTheListCache(t *testing.T) {
	var listCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			listCount++
			if err := json.NewEncoder(w).Encode([]Team{{ID: 5, Name: "Security"}}); err != nil {
				t.Errorf("encoding response: %v", err)
			}
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	apiClient := testClient(srv)

	if _, err := All(context.Background(), apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}
	if err := Update(context.Background(), apiClient, 5, "Security", &[]int64{1}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := All(context.Background(), apiClient); err != nil {
		t.Fatalf("All after Update: %v", err)
	}

	if listCount != 2 {
		t.Errorf("list requested %d times, want 2: Update must invalidate the cache", listCount)
	}
}

func TestIsImported(t *testing.T) {
	if !IsImported(Team{ExternalSource: "github"}) {
		t.Error("github team reported as manual")
	}
	if IsImported(Team{}) {
		t.Error("team without an external source reported as imported")
	}
}

func TestCodeRepoIDs_OnlyCodeRepositoriesSorted(t *testing.T) {
	team := Team{Responsibilities: []Responsibility{
		{ID: 9, Type: TypeCodeRepository},
		{ID: 3, Type: "cloud"},
		{ID: 4, Type: TypeCodeRepository},
		{ID: 8, Type: "container_repository"},
	}}

	ids := CodeRepoIDs(team)
	if len(ids) != 2 || ids[0] != 4 || ids[1] != 9 {
		t.Errorf("CodeRepoIDs = %v, want [4 9]", ids)
	}
}

// The update endpoint diffs code repositories only, so path limitations are the
// sole part of a team's responsibilities Terraform cannot see or manage.
func TestPathLimited(t *testing.T) {
	tests := []struct {
		name  string
		team  Team
		count int
	}{
		{
			name:  "plain code repositories carry no limits",
			team:  Team{Responsibilities: []Responsibility{{ID: 1, Type: TypeCodeRepository}}},
			count: 0,
		},
		{
			name: "other responsibility types are untouched by an update",
			team: Team{Responsibilities: []Responsibility{
				{ID: 1, Type: TypeCodeRepository},
				{ID: 2, Type: "cloud"},
				{ID: 3, Type: "container_repository"},
				{ID: 4, Type: "domain"},
				{ID: 5, Type: "zen_app"},
			}},
			count: 0,
		},
		{
			name: "included paths count",
			team: Team{Responsibilities: []Responsibility{
				{ID: 1, Type: TypeCodeRepository, IncludedPaths: []string{"/client"}},
			}},
			count: 1,
		},
		{
			name: "excluded paths count",
			team: Team{Responsibilities: []Responsibility{
				{ID: 1, Type: TypeCodeRepository, ExcludedPaths: []string{"/vendor"}},
			}},
			count: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PathLimited(test.team); len(got) != test.count {
				t.Errorf("PathLimited = %v, want %d entries", got, test.count)
			}
		})
	}
}
