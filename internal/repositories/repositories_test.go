package repositories

import (
	"context"
	"encoding/json"
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

func TestByID_MissingRepositoryIsNotFound(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Repository{ID: 1})

	_, err := ByID(context.Background(), testClient(srv), 999)
	if err == nil {
		t.Fatal("ByID: want error for missing repository, got nil")
	}
	if !client.NotFound(err) {
		t.Errorf("err = %v, want a not-found API error", err)
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
