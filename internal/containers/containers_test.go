package containers

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

// The list endpoint documents linked_code_repo_id as an integer and the detail
// endpoint as a string or null. A whole page decodes in one call, so a shape this
// type rejects fails every container read.
func TestLinkedCodeRepoID_DecodesNumbersStringsAndNull(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *int64
	}{
		{"number", `{"id":1,"linked_code_repo_id":67}`, ptr(int64(67))},
		{"quoted number", `{"id":1,"linked_code_repo_id":"67"}`, ptr(int64(67))},
		{"null", `{"id":1,"linked_code_repo_id":null}`, nil},
		{"absent", `{"id":1}`, nil},
		// The API stores "not linked" as a non-positive sentinel rather than null.
		{"unlink sentinel", `{"id":1,"linked_code_repo_id":-1}`, nil},
		{"zero sentinel", `{"id":1,"linked_code_repo_id":0}`, nil},
		{"quoted sentinel", `{"id":1,"linked_code_repo_id":"-1"}`, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var container Container
			if err := json.Unmarshal([]byte(tt.body), &container); err != nil {
				t.Fatalf("decoding %s: %v", tt.body, err)
			}

			switch {
			case tt.want == nil && container.LinkedCodeRepoID != nil:
				t.Errorf("LinkedCodeRepoID = %d, want nil", *container.LinkedCodeRepoID)
			case tt.want != nil && container.LinkedCodeRepoID == nil:
				t.Errorf("LinkedCodeRepoID = nil, want %d", *tt.want)
			case tt.want != nil && *container.LinkedCodeRepoID != *tt.want:
				t.Errorf("LinkedCodeRepoID = %d, want %d", *container.LinkedCodeRepoID, *tt.want)
			}
		})
	}
}

// Null registry_id and cloud_id must stay nil rather than becoming 0: a 0 would
// read as a real ID and mis-scope a data source filter.
func TestNullableIDs_StayNil(t *testing.T) {
	var container Container
	body := `{"id":1,"registry_id":null,"cloud_id":null}`

	if err := json.Unmarshal([]byte(body), &container); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if container.RegistryID != nil {
		t.Errorf("RegistryID = %d, want nil", *container.RegistryID)
	}
	if container.CloudID != nil {
		t.Errorf("CloudID = %d, want nil", *container.CloudID)
	}
}

// The provider value is derived from a registry kind, a cloud type or a fallback,
// so the set is open ended. An unexpected value must reach state rather than fail
// the decode.
func TestUndocumentedProviderValueDecodes(t *testing.T) {
	var container Container

	if err := json.Unmarshal([]byte(`{"id":1,"provider":"aws_ecr"}`), &container); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if container.Provider != "aws_ecr" {
		t.Errorf("Provider = %q, want aws_ecr", container.Provider)
	}
}

func TestContainer_DecodesTagAsTagFilterAndIsActiveAsActive(t *testing.T) {
	var container Container
	body := `{"id":1,"tag":"prod-*","last_scanned_tag":"prod-20250623","is_active":true}`

	if err := json.Unmarshal([]byte(body), &container); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if container.TagFilter != "prod-*" {
		t.Errorf("TagFilter = %q, want prod-*", container.TagFilter)
	}
	if container.LastScannedTag != "prod-20250623" {
		t.Errorf("LastScannedTag = %q, want prod-20250623", container.LastScannedTag)
	}
	if !container.Active {
		t.Error("Active = false, want true")
	}
}

func ptr[T any](v T) *T {
	return &v
}

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

// listServer serves one page of containers and counts how often it is called.
func listServer(t *testing.T, requestCount *int, items ...Container) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != BasePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		*requestCount++
		if err := json.NewEncoder(w).Encode(items); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestAll_SortsByIDRegardlessOfAPIOrder(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount,
		Container{ID: 30, Name: "gamma"},
		Container{ID: 10, Name: "alpha"},
		Container{ID: 20, Name: "beta"},
	)

	all, err := All(context.Background(), testClient(srv))
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	wantIDs := []int64{10, 20, 30}
	if len(all) != len(wantIDs) {
		t.Fatalf("got %d containers, want %d", len(all), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if all[i].ID != wantID {
			t.Errorf("position %d = id %d, want %d", i, all[i].ID, wantID)
		}
	}
}

func TestAll_AndByID_ShareOneCachedFetch(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Container{ID: 1, Name: "compression", Active: true})
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
	srv := listServer(t, &requestCount, Container{ID: 1, Name: "compression", Active: true})
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

// A resource that writes one container must not cost every other resource a
// fresh paginated list, so a write refreshes the cached entry in place.
func TestStoreCached_RefreshesOneEntryWithoutRefetching(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount,
		Container{ID: 1, Name: "compression", TagFilter: "", Active: false},
		Container{ID: 2, Name: "decompression", Active: true},
	)
	apiClient := testClient(srv)
	ctx := context.Background()

	if _, err := ByID(ctx, apiClient, 1); err != nil {
		t.Fatalf("ByID: %v", err)
	}

	StoreCached(apiClient, Container{ID: 1, Name: "compression", TagFilter: "prod-*", Active: true})

	updated, err := ByID(ctx, apiClient, 1)
	if err != nil {
		t.Fatalf("ByID (after store): %v", err)
	}
	if updated.TagFilter != "prod-*" || !updated.Active {
		t.Errorf("container 1 = %+v, want tag filter prod-* and active", updated)
	}

	untouched, err := ByID(ctx, apiClient, 2)
	if err != nil {
		t.Fatalf("ByID 2: %v", err)
	}
	if untouched.Name != "decompression" {
		t.Errorf("container 2 = %+v, want it left alone", untouched)
	}

	if requestCount != 1 {
		t.Errorf("list endpoint hit %d times, want 1", requestCount)
	}
}

// Nothing cached means the next read fetches fresh data, so a store before any
// read must not seed a one-container list that hides every other container.
func TestStoreCached_DoesNotSeedAnEmptyCache(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Container{ID: 1, Name: "compression"}, Container{ID: 2, Name: "decompression"})
	apiClient := testClient(srv)

	StoreCached(apiClient, Container{ID: 1, Name: "renamed"})

	all, err := All(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("containers = %d, want 2", len(all))
	}
	if all[0].Name != "compression" {
		t.Errorf("container 1 = %q, want the value the API returned", all[0].Name)
	}
}

// Every opt-in group must be requested. filter_status defaults to active, and
// labels, sensitivity and connectivity are omitted unless asked for; a missing
// flag makes the resource read back nulls and churn on every plan.
func TestAll_RequestsInactiveLabelsSensitivityAndConnectivity(t *testing.T) {
	var query string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	if _, err := All(context.Background(), testClient(srv)); err != nil {
		t.Fatalf("All: %v", err)
	}

	for _, want := range []string{
		"filter_status=all",
		"include_labels=true",
		"include_sensitivity=true",
		"include_connectivity=true",
		"per_page=100",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}
}

// A workspace holding exactly one full page must still be asked for a second
// page: FetchAllPages stops on a short page, so a full last page looks like more.
func TestAll_FetchesASecondPageWhenTheFirstIsExactlyFull(t *testing.T) {
	var pages int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if pages == 1 {
			full := make([]Container, 0, 100)
			for i := 1; i <= 100; i++ {
				full = append(full, Container{ID: int64(i)})
			}
			_ = json.NewEncoder(w).Encode(full)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	all, err := All(context.Background(), testClient(srv))
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if pages != 2 {
		t.Errorf("fetched %d pages, want 2", pages)
	}
	if len(all) != 100 {
		t.Errorf("got %d containers, want 100", len(all))
	}
}

func TestByID_MissingContainerIsNotInList(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Container{ID: 1})

	_, err := ByID(context.Background(), testClient(srv), 999)
	if err == nil {
		t.Fatal("ByID: want error for missing container, got nil")
	}
	if !client.NotInList(err) {
		t.Errorf("err = %v, want a not-in-list error", err)
	}
	// Distinguishable from a failed request, which callers must not treat as
	// proof the container is gone.
	if client.NotFound(err) {
		t.Errorf("err = %v, must not also read as an API 404", err)
	}
}

// A cloud-sourced container carries no registry and vice versa. Both must survive
// the list round-trip as nil.
func TestAll_PreservesNullRegistryAndCloudIDs(t *testing.T) {
	var requestCount int
	srv := listServer(t, &requestCount, Container{ID: 1, RegistryID: ptr(int64(3))}, Container{ID: 2, CloudID: ptr(int64(9))})

	all, err := All(context.Background(), testClient(srv))
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if all[0].RegistryID == nil || *all[0].RegistryID != 3 || all[0].CloudID != nil {
		t.Errorf("registry-sourced container = %#v", all[0])
	}
	if all[1].CloudID == nil || *all[1].CloudID != 9 || all[1].RegistryID != nil {
		t.Errorf("cloud-sourced container = %#v", all[1])
	}
}
