package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
)

// Scale proof only: old Read = 1 GET per TF resource; new Read = paginate list once.
// Pagination, cache-once, and post-write detail/filter GETs are covered in smaller unit tests.
const (
	reposInWorkspace          = 13_000
	reposPerListPage          = 200
	repoListRequestCount      = reposInWorkspace/reposPerListPage + 1 // 66
	prChecksPerListPage       = 100
	prChecksListRequestCount  = reposInWorkspace/prChecksPerListPage + 1 // 131
	parallelWorkers           = 10
	resourcesInTerraformState = 500
)

type apiCallCounts struct {
	listPages      atomic.Int32
	perResourceGET atomic.Int32
}

func (counts *apiCallCounts) snapshot() (listPages, perResourceGET int32) {
	return counts.listPages.Load(), counts.perResourceGET.Load()
}

func (counts *apiCallCounts) allGETs() int32 {
	return counts.listPages.Load() + counts.perResourceGET.Load()
}

func writeRepoListPage(t *testing.T, response http.ResponseWriter, request *http.Request) {
	t.Helper()

	pageNumber, _ := strconv.Atoi(request.URL.Query().Get("page"))
	firstIndex := pageNumber * reposPerListPage

	if firstIndex >= reposInWorkspace {
		writeReposList(t, response)
		return
	}

	lastIndex := min(firstIndex+reposPerListPage, reposInWorkspace)
	page := make([]repositories.Repository, 0, lastIndex-firstIndex)

	for repoID := firstIndex + 1; repoID <= lastIndex; repoID++ {
		page = append(page, repositories.Repository{ID: int64(repoID), Name: "repo", Active: true})
	}

	writeReposList(t, response, page...)
}

func writePRChecksListPage(t *testing.T, response http.ResponseWriter, request *http.Request) {
	t.Helper()

	pageNumber, _ := strconv.Atoi(request.URL.Query().Get("page"))
	firstIndex := pageNumber * prChecksPerListPage

	if firstIndex >= reposInWorkspace {
		_ = json.NewEncoder(response).Encode([]prChecksSettingsAPI{})
		return
	}

	lastIndex := min(firstIndex+prChecksPerListPage, reposInWorkspace)
	page := make([]prChecksSettingsAPI, 0, lastIndex-firstIndex)

	for repoID := firstIndex + 1; repoID <= lastIndex; repoID++ {
		page = append(page, prChecksSettingsAPI{
			ID:              int64(repoID),
			CodeRepoID:      int64(repoID),
			MinimumSeverity: "high",
		})
	}

	_ = json.NewEncoder(response).Encode(page)
}

func newRepoAPIServer(t *testing.T, counts *apiCallCounts) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case isCodeReposList(request):
			counts.listPages.Add(1)
			writeRepoListPage(t, response, request)

		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/public/v1/repositories/code/"):
			counts.perResourceGET.Add(1)
			repoID, _ := strconv.ParseInt(strings.TrimPrefix(request.URL.Path, "/public/v1/repositories/code/"), 10, 64)
			_ = json.NewEncoder(response).Encode(repositories.Repository{ID: repoID, Name: "repo", Active: true})

		default:
			t.Errorf("unexpected %s %s", request.Method, request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newPRChecksAPIServer(t *testing.T, counts *apiCallCounts) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != prChecksSettingsPath || request.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", request.Method, request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
			return
		}

		if filterRepoID := request.URL.Query().Get("filter_code_repo_id"); filterRepoID != "" {
			counts.perResourceGET.Add(1)

			repoID, _ := strconv.ParseInt(filterRepoID, 10, 64)
			_ = json.NewEncoder(response).Encode([]prChecksSettingsAPI{{
				ID:              repoID,
				CodeRepoID:      repoID,
				MinimumSeverity: "critical",
			}})
			return
		}

		counts.listPages.Add(1)
		writePRChecksListPage(t, response, request)
	}))
}

func forEachManagedResource(t *testing.T, fn func(resourceIndex int) error) {
	t.Helper()

	slots := make(chan struct{}, parallelWorkers)
	var waitGroup sync.WaitGroup
	errors := make(chan error, resourcesInTerraformState)

	for resourceIndex := 1; resourceIndex <= resourcesInTerraformState; resourceIndex++ {
		waitGroup.Add(1)
		slots <- struct{}{}

		go func(resourceIndex int) {
			defer waitGroup.Done()
			defer func() { <-slots }()

			if err := fn(resourceIndex); err != nil {
				errors <- err
			}
		}(resourceIndex)
	}

	waitGroup.Wait()
	close(errors)

	for err := range errors {
		t.Fatal(err)
	}
}

func TestLargeWorkspace_RepositoryRefresh_OldVsCached(t *testing.T) {
	ctx := context.Background()

	// Old path: 1 detail GET per resource in state.
	var oldPath apiCallCounts
	oldServer := newRepoAPIServer(t, &oldPath)
	t.Cleanup(oldServer.Close)
	oldAPI := testClient(oldServer)

	forEachManagedResource(t, func(resourceIndex int) error {
		_, err := repositories.Detail(ctx, oldAPI, int64(resourceIndex))
		return err
	})

	oldListPages, oldDetailGETs := oldPath.snapshot()
	if oldListPages != 0 || oldDetailGETs != resourcesInTerraformState {
		t.Fatalf("old refresh: listPages=%d detailGETs=%d, want 0 and %d",
			oldListPages, oldDetailGETs, resourcesInTerraformState)
	}

	// Cached path: paginate once, then memory lookups.
	var cachedPath apiCallCounts
	cachedServer := newRepoAPIServer(t, &cachedPath)
	t.Cleanup(cachedServer.Close)
	cachedAPI := testClient(cachedServer)

	forEachManagedResource(t, func(resourceIndex int) error {
		_, err := repositories.ByID(ctx, cachedAPI, int64(resourceIndex))
		return err
	})

	cachedListPages, cachedDetailGETs := cachedPath.snapshot()
	if cachedListPages != repoListRequestCount || cachedDetailGETs != 0 {
		t.Fatalf("cached refresh: listPages=%d detailGETs=%d, want %d and 0",
			cachedListPages, cachedDetailGETs, repoListRequestCount)
	}

	if cachedPath.allGETs() >= oldPath.allGETs() {
		t.Fatalf("cached refresh used %d GETs, old path used %d", cachedPath.allGETs(), oldPath.allGETs())
	}
}

func TestLargeWorkspace_PRChecksRefresh_OldVsCached(t *testing.T) {
	ctx := context.Background()

	var oldPath apiCallCounts
	oldServer := newPRChecksAPIServer(t, &oldPath)
	t.Cleanup(oldServer.Close)
	oldAPI := testClient(oldServer)

	forEachManagedResource(t, func(resourceIndex int) error {
		settings, err := prChecksSettingsFromAPI(ctx, oldAPI, int64(resourceIndex))
		if err != nil {
			return err
		}
		if settings == nil {
			return fmt.Errorf("missing PR checks for repo %d", resourceIndex)
		}
		return nil
	})

	oldListPages, oldFilterGETs := oldPath.snapshot()
	if oldListPages != 0 || oldFilterGETs != resourcesInTerraformState {
		t.Fatalf("old refresh: listPages=%d filterGETs=%d, want 0 and %d",
			oldListPages, oldFilterGETs, resourcesInTerraformState)
	}

	var cachedPath apiCallCounts
	cachedServer := newPRChecksAPIServer(t, &cachedPath)
	t.Cleanup(cachedServer.Close)
	cachedAPI := testClient(cachedServer)

	forEachManagedResource(t, func(resourceIndex int) error {
		settings, err := prChecksSettingsFromCache(ctx, cachedAPI, int64(resourceIndex))
		if err != nil {
			return err
		}
		if settings == nil {
			return fmt.Errorf("missing PR checks for repo %d", resourceIndex)
		}
		return nil
	})

	cachedListPages, cachedFilterGETs := cachedPath.snapshot()
	if cachedListPages != prChecksListRequestCount || cachedFilterGETs != 0 {
		t.Fatalf("cached refresh: listPages=%d filterGETs=%d, want %d and 0",
			cachedListPages, cachedFilterGETs, prChecksListRequestCount)
	}

	if cachedPath.allGETs() >= oldPath.allGETs() {
		t.Fatalf("cached refresh used %d GETs, old path used %d", cachedPath.allGETs(), oldPath.allGETs())
	}
}
