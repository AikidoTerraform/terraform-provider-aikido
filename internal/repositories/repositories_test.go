package repositories

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"golang.org/x/time/rate"
)

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

// listServer serves one page of repositories and counts how often it is called.
func listServer(t *testing.T, requestCount *int, repos ...Repository) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != BasePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		*requestCount++
		if err := json.NewEncoder(w).Encode(repos); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestAll_SortsByIDRegardlessOfAPIOrder(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount,
		Repository{ID: 30, Name: "gamma"},
		Repository{ID: 10, Name: "alpha"},
		Repository{ID: 20, Name: "beta"},
	)

	all, err := All(context.Background(), testClient(srv))
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	wantIDs := []int64{10, 20, 30}
	if len(all) != len(wantIDs) {
		t.Fatalf("got %d repositories, want %d", len(all), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if all[i].ID != wantID {
			t.Errorf("position %d = id %d, want %d", i, all[i].ID, wantID)
		}
	}
}

func TestAll_AndByID_ShareOneCachedFetch(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Repository{ID: 1, Name: "payments", Active: true})
	apiClient := testClient(srv)
	ctx := context.Background()

	if _, err := All(ctx, apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}
	if _, err := ByID(ctx, apiClient, 1); err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if _, err := All(ctx, apiClient); err != nil {
		t.Fatalf("All (second call): %v", err)
	}

	if requestCount != 1 {
		t.Errorf("list endpoint hit %d times, want 1", requestCount)
	}
}

func TestInvalidateCache_MakesTheNextReadRefetch(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Repository{ID: 1, Name: "payments", Active: true})
	apiClient := testClient(srv)
	ctx := context.Background()

	if _, err := All(ctx, apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}
	InvalidateCache(apiClient)
	if _, err := All(ctx, apiClient); err != nil {
		t.Fatalf("All (after invalidate): %v", err)
	}

	if requestCount != 2 {
		t.Errorf("list endpoint hit %d times, want 2", requestCount)
	}
}

func TestAll_RequestsInactiveAndLabels(t *testing.T) {
	var query string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	if _, err := All(context.Background(), testClient(srv)); err != nil {
		t.Fatalf("All: %v", err)
	}

	for _, want := range []string{"include_inactive=true", "include_labels=true"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}
}

func TestByID_MissingRepositoryIsNotInList(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Repository{ID: 1})

	_, err := ByID(context.Background(), testClient(srv), 999)
	if err == nil {
		t.Fatal("ByID: want error for missing repository, got nil")
	}
	if !client.NotInList(err) {
		t.Errorf("err = %v, want a not-in-list error", err)
	}
	// Distinguishable from a failed request, which callers must not treat as
	// proof the repository is gone.
	if client.NotFound(err) {
		t.Errorf("err = %v, must not also read as an API 404", err)
	}
}

// ByName pushes the name to the API, so a lookup in a large workspace never
// pages through every repository.
func TestByName_FiltersServerSideAndRequestsInactiveAndLabels(t *testing.T) {
	var query url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]Repository{{ID: 1, Name: "payments & billing"}})
	}))
	t.Cleanup(srv.Close)

	matched, err := ByName(context.Background(), testClient(srv), "payments & billing")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}

	if got := query.Get("filter_name"); got != "payments & billing" {
		t.Errorf("filter_name = %q, want the name, escaped so the API decodes it unchanged", got)
	}
	for _, want := range []string{"include_inactive", "include_labels"} {
		if query.Get(want) != "true" {
			t.Errorf("query %v missing %s=true", query, want)
		}
	}
	if len(matched) != 1 || matched[0].ID != 1 {
		t.Errorf("matched = %#v, want repository 1", matched)
	}
}

// The API does not promise filter_name is exact, so a server that treats it as
// a substring must not leak near-misses into a single-repository lookup.
func TestByName_KeepsOnlyExactMatchesSortedByID(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount,
		Repository{ID: 30, Name: "payments", Branch: "develop"},
		Repository{ID: 20, Name: "payments-legacy", Branch: "main"},
		Repository{ID: 10, Name: "payments", Branch: "main"},
		Repository{ID: 40, Name: "Payments", Branch: "main"},
	)

	matched, err := ByName(context.Background(), testClient(srv), "payments")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}

	wantIDs := []int64{10, 30}
	if len(matched) != len(wantIDs) {
		t.Fatalf("got %d repositories %#v, want %d", len(matched), matched, len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if matched[i].ID != wantID {
			t.Errorf("position %d = id %d, want %d", i, matched[i].ID, wantID)
		}
	}
}

func TestByName_NoMatchIsEmptyNotNil(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount)

	matched, err := ByName(context.Background(), testClient(srv), "payments")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}
	if matched == nil || len(matched) != 0 {
		t.Errorf("matched = %#v, want an empty non-nil slice", matched)
	}
}

// ByName reads the API directly: a stale shared list must not hide a repository
// that was renamed or activated by a resource earlier in the same apply, and a
// lookup must not warm the cache with a filtered page.
func TestByName_BypassesTheSharedListCache(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Repository{ID: 1, Name: "payments"})
	apiClient := testClient(srv)
	ctx := context.Background()

	if _, err := ByName(ctx, apiClient, "payments"); err != nil {
		t.Fatalf("ByName: %v", err)
	}
	if _, err := ByName(ctx, apiClient, "payments"); err != nil {
		t.Fatalf("ByName (second call): %v", err)
	}
	if requestCount != 2 {
		t.Errorf("list endpoint hit %d times after two lookups, want 2", requestCount)
	}

	if _, err := All(ctx, apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}
	if requestCount != 3 {
		t.Errorf("list endpoint hit %d times after All, want 3: a filtered lookup must not populate the cache", requestCount)
	}
}

func TestByName_PropagatesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	if _, err := ByName(context.Background(), testClient(srv), "payments"); err == nil {
		t.Fatal("ByName: want error on 403, got nil")
	}
}

func TestDetail_UsesDetailPath(t *testing.T) {
	var path string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(Repository{ID: 42, Name: "payments", Active: true})
	}))
	t.Cleanup(srv.Close)

	repo, err := Detail(context.Background(), testClient(srv), 42)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	if path != BasePath+"/42" {
		t.Errorf("path = %s, want %s/42", path, BasePath)
	}
	if repo.ID != 42 || !repo.Active {
		t.Errorf("repo = %#v", repo)
	}
}

// Labels nested in a repository decode through the shared label decoder.
func TestRepository_DecodesNestedLabels(t *testing.T) {
	var repo Repository
	body := `{"id":9,"labels":[{"id":7,"name":"imported","is_imported":true}]}`

	if err := json.Unmarshal([]byte(body), &repo); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if len(repo.Labels) != 1 {
		t.Fatalf("labels = %#v, want one", repo.Labels)
	}
	if got := repo.Labels[0]; got.ID != "7" || got.Name != "imported" || !got.IsImported {
		t.Errorf("label = %#v", got)
	}
}
