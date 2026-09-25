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
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTeamResourceID(t *testing.T) {
	if got := teamResourceID(123, teams.KindRepo, 4); got != "123:repo:4" {
		t.Errorf("teamResourceID = %q, want 123:repo:4", got)
	}
}

func TestParseTeamResourceID(t *testing.T) {
	t.Run("a composite id yields all three parts", func(t *testing.T) {
		teamID, kind, resourceID, err := parseTeamResourceID("123:cloud:12")
		if err != nil {
			t.Fatalf("parseTeamResourceID: %v", err)
		}
		if teamID != 123 || kind != teams.KindCloud || resourceID != 12 {
			t.Errorf("got %d, %s, %d, want 123, cloud, 12", teamID, kind, resourceID)
		}
	})

	t.Run("malformed ids are rejected with the expected format", func(t *testing.T) {
		for _, id := range []string{"", "123", "123:repo", "a:repo:4", "123:unknown:4", "1:repo:2:3"} {
			t.Run(id, func(t *testing.T) {
				_, _, _, err := parseTeamResourceID(id)
				if err == nil {
					t.Fatalf("parseTeamResourceID(%q) returned no error", id)
				}
				if !strings.Contains(err.Error(), "team_id:kind:resource_id") {
					t.Errorf("error %q does not state the expected format", err)
				}
			})
		}
	})
}

func pathLimitation(limitationType string, paths ...string) *repoPathLimitationModel {
	elements := make([]attr.Value, 0, len(paths))
	for _, pathValue := range paths {
		elements = append(elements, types.StringValue(pathValue))
	}

	return &repoPathLimitationModel{
		LimitationType: types.StringValue(limitationType),
		Paths:          types.ListValueMust(types.StringType, elements),
	}
}

func linkedAPIServer(t *testing.T, calls *[]string, team teams.Team) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == teams.BasePath:
			_ = json.NewEncoder(w).Encode([]teams.Team{team})
		default:
			_, _ = io.WriteString(w, `{"success":1}`)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

var linkedManualTeam = teams.Team{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
	{ID: 4, Type: teams.TypeCodeRepository, IncludedPaths: []string{"/client/"}},
	{ID: 12, Type: teams.TypeCloud},
}}

func TestCreateLink(t *testing.T) {
	t.Run("links a cloud", func(t *testing.T) {
		var calls []string
		var gotBody string
		listed := teams.Team{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
			{ID: 12, Type: teams.TypeCloud},
		}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{listed})
				return
			}
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_, _ = io.WriteString(w, `{"success":1}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		state, diagnostics := res.createLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(12),
		})
		if diagnostics.HasError() {
			t.Fatalf("createLink: %v", diagnostics)
		}

		if !strings.Contains(gotBody, `"cloud_id":12`) {
			t.Errorf("body = %s, want the cloud id", gotBody)
		}
		if !slicesContains(calls, "POST "+teams.BasePath+"/123/linkResource") {
			t.Errorf("calls = %v, want the linkResource POST", calls)
		}
		if state.ID != types.StringValue("123:cloud:12") {
			t.Errorf("ID = %v, want \"123:cloud:12\"", state.ID)
		}
	})

	t.Run("links a repository with path limitations", func(t *testing.T) {
		var gotBody string
		listed := teams.Team{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
			{ID: 4, Type: teams.TypeCodeRepository, IncludedPaths: []string{"/client/"}},
		}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{listed})
				return
			}
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_, _ = io.WriteString(w, `{"success":1}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		state, diagnostics := res.createLink(context.Background(), teamLinkedModel{
			TeamID:             types.Int64Value(123),
			RepoID:             types.Int64Value(4),
			RepoPathLimitation: pathLimitation(teams.LimitationInclude, "/client/"),
		})
		if diagnostics.HasError() {
			t.Fatalf("createLink: %v", diagnostics)
		}
		if !strings.Contains(gotBody, `"repo_id":4`) || !strings.Contains(gotBody, `"limitation_type":"include"`) {
			t.Errorf("body = %s, want the repo and its path limitation", gotBody)
		}
		if state.RepoPathLimitation == nil || state.RepoPathLimitation.LimitationType != types.StringValue(teams.LimitationInclude) {
			t.Errorf("RepoPathLimitation = %v, want include /client/", state.RepoPathLimitation)
		}
	})

	t.Run("an imported team is rejected before the write", func(t *testing.T) {
		var calls []string
		srv := linkedAPIServer(t, &calls, teams.Team{ID: 123, Name: "Frontend developers", ExternalSource: "github"})

		res := &teamLinkedResource{client: testClient(srv)}
		_, diagnostics := res.createLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(12),
		})

		if !diagnostics.HasError() {
			t.Fatal("link on an imported team produced no error")
		}
		for _, call := range calls {
			if strings.Contains(call, "linkResource") {
				t.Errorf("calls = %v, want no write", calls)
			}
		}
	})
}

func TestCreateLink_ReconcilesAnAmbiguousLink(t *testing.T) {
	t.Run("a link that landed despite the error succeeds", func(t *testing.T) {
		listed := []teams.Team{{ID: 123, Name: "Payments"}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode(listed)
				return
			}
			listed = []teams.Team{{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
				{ID: 12, Type: teams.TypeCloud},
			}}}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		state, diagnostics := res.createLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(12),
		})

		if diagnostics.HasError() {
			t.Fatalf("got %v, want the link to be recognised as created", diagnostics)
		}
		if state.ID != types.StringValue("123:cloud:12") {
			t.Errorf("ID = %v, want \"123:cloud:12\"", state.ID)
		}
	})

	t.Run("a rejected link on an already linked resource fails", func(t *testing.T) {
		linkedWithoutPaths := teams.Team{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
			{ID: 4, Type: teams.TypeCodeRepository},
		}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{linkedWithoutPaths})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"reason_phrase":"repo already linked"}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		_, diagnostics := res.createLink(context.Background(), teamLinkedModel{
			TeamID:             types.Int64Value(123),
			RepoID:             types.Int64Value(4),
			RepoPathLimitation: pathLimitation(teams.LimitationInclude, "/client/"),
		})

		if !diagnostics.HasError() {
			t.Fatal("a rejected link whose responsibility predates the write produced no error")
		}
		if detail := diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "123:repo:4") {
			t.Errorf("detail %q does not point at the existing link", detail)
		}
	})

	t.Run("a link that did not land still fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{linkedManualTeam})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"reason_phrase":"cloud not found"}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		_, diagnostics := res.createLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(99),
		})

		if !diagnostics.HasError() {
			t.Fatal("a link that never landed produced no error")
		}
		if !strings.Contains(diagnostics.Errors()[0].Detail(), "cloud not found") {
			t.Errorf("detail %q drops the API's reason", diagnostics.Errors()[0].Detail())
		}
	})
}

func TestCreateLink_KeepsTheIDWhenReadBackFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "linkResource") {
			_, _ = io.WriteString(w, `{"success":1}`)
			return
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]teams.Team{{ID: 123, Name: "Payments"}})
			return
		}
	}))
	t.Cleanup(srv.Close)

	res := &teamLinkedResource{client: testClient(srv)}
	state, diagnostics := res.createLink(context.Background(), teamLinkedModel{
		TeamID:  types.Int64Value(123),
		CloudID: types.Int64Value(12),
	})

	if !diagnostics.HasError() {
		t.Fatal("a missing responsibility after the write produced no error")
	}
	if state.ID != types.StringValue("123:cloud:12") {
		t.Errorf("ID = %v, want \"123:cloud:12\" so the link stays tracked", state.ID)
	}
}

func TestUpdateLink_UpdatesPathLimitation(t *testing.T) {
	var gotPath, gotBody string
	listed := teams.Team{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
		{ID: 4, Type: teams.TypeCodeRepository, ExcludedPaths: []string{"/vendor/"}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == teams.BasePath {
			_ = json.NewEncoder(w).Encode([]teams.Team{listed})
			return
		}
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	res := &teamLinkedResource{client: testClient(srv)}
	state, diagnostics := res.updateLink(context.Background(), teamLinkedModel{
		TeamID:             types.Int64Value(123),
		RepoID:             types.Int64Value(4),
		RepoPathLimitation: pathLimitation(teams.LimitationExclude, "/vendor/"),
	}, teamLinkedModel{
		TeamID:             types.Int64Value(123),
		RepoID:             types.Int64Value(4),
		RepoPathLimitation: pathLimitation(teams.LimitationInclude, "/client/"),
	})
	if diagnostics.HasError() {
		t.Fatalf("updateLink: %v", diagnostics)
	}
	if gotPath != teams.BasePath+"/123/updateRepoPathLimitation" {
		t.Errorf("path = %s, want updateRepoPathLimitation", gotPath)
	}
	if !strings.Contains(gotBody, `"limitation_type":"exclude"`) {
		t.Errorf("body = %s, want the new limitation", gotBody)
	}
	if state.RepoPathLimitation == nil || state.RepoPathLimitation.LimitationType != types.StringValue(teams.LimitationExclude) {
		t.Errorf("RepoPathLimitation = %v, want exclude", state.RepoPathLimitation)
	}
}

func TestUpdateLink_ClearsPathLimitationWhenOmitted(t *testing.T) {
	var gotBody string
	listed := teams.Team{ID: 123, Name: "Payments", Responsibilities: []teams.Responsibility{
		{ID: 4, Type: teams.TypeCodeRepository},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == teams.BasePath {
			_ = json.NewEncoder(w).Encode([]teams.Team{listed})
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	res := &teamLinkedResource{client: testClient(srv)}
	state, diagnostics := res.updateLink(context.Background(), teamLinkedModel{
		TeamID: types.Int64Value(123),
		RepoID: types.Int64Value(4),
	}, teamLinkedModel{
		TeamID:             types.Int64Value(123),
		RepoID:             types.Int64Value(4),
		RepoPathLimitation: pathLimitation(teams.LimitationInclude, "/client/"),
	})
	if diagnostics.HasError() {
		t.Fatalf("updateLink: %v", diagnostics)
	}
	if !strings.Contains(gotBody, `"paths":[]`) {
		t.Errorf("body = %s, want an empty paths list that clears the filter", gotBody)
	}
	if state.RepoPathLimitation != nil {
		t.Errorf("RepoPathLimitation = %v, want nil after clearing", state.RepoPathLimitation)
	}
}

func TestDeleteLink(t *testing.T) {
	t.Run("unlinks the resource from a manual team", func(t *testing.T) {
		var calls []string
		var gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			if r.URL.Path == teams.BasePath {
				_ = json.NewEncoder(w).Encode([]teams.Team{linkedManualTeam})
				return
			}
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_, _ = io.WriteString(w, `{"success":1}`)
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		if diagnostics := res.deleteLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(12),
		}); diagnostics.HasError() {
			t.Fatalf("deleteLink: %v", diagnostics)
		}
		if !slicesContains(calls, "POST "+teams.BasePath+"/123/unlinkResource") {
			t.Errorf("calls = %v, want the unlinkResource POST", calls)
		}
		if !strings.Contains(gotBody, `"cloud_id":12`) {
			t.Errorf("body = %s, want the cloud id", gotBody)
		}
	})

	t.Run("an imported team is refused before the write", func(t *testing.T) {
		var calls []string
		srv := linkedAPIServer(t, &calls, teams.Team{ID: 123, Name: "Frontend developers", ExternalSource: "github"})

		res := &teamLinkedResource{client: testClient(srv)}
		diagnostics := res.deleteLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(12),
		})

		if !diagnostics.HasError() {
			t.Fatal("destroying a link on an imported team produced no error")
		}
		for _, call := range calls {
			if strings.Contains(call, "unlinkResource") {
				t.Errorf("calls = %v, want no write", calls)
			}
		}
	})

	t.Run("a team that no longer exists is not an error", func(t *testing.T) {
		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			_ = json.NewEncoder(w).Encode([]teams.Team{})
		}))
		t.Cleanup(srv.Close)

		res := &teamLinkedResource{client: testClient(srv)}
		if diagnostics := res.deleteLink(context.Background(), teamLinkedModel{
			TeamID:  types.Int64Value(123),
			CloudID: types.Int64Value(12),
		}); diagnostics.HasError() {
			t.Fatalf("got %v, want no error", diagnostics)
		}
		for _, call := range calls {
			if strings.Contains(call, "unlinkResource") {
				t.Errorf("calls = %v, want no write", calls)
			}
		}
	})
}

func TestCreateLink_InvalidatesTheTeamCache(t *testing.T) {
	var listCount int
	listed := teams.Team{ID: 123, Name: "Payments"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == teams.BasePath {
			listCount++
			_ = json.NewEncoder(w).Encode([]teams.Team{listed})
			return
		}
		listed.Responsibilities = []teams.Responsibility{{ID: 12, Type: teams.TypeCloud}}
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	res := &teamLinkedResource{client: testClient(srv)}
	if _, err := teams.ByID(context.Background(), res.client, 123); err != nil {
		t.Fatalf("ByID: %v", err)
	}

	if _, diagnostics := res.createLink(context.Background(), teamLinkedModel{
		TeamID:  types.Int64Value(123),
		CloudID: types.Int64Value(12),
	}); diagnostics.HasError() {
		t.Fatalf("createLink: %v", diagnostics)
	}

	if listCount != 2 {
		t.Errorf("list requested %d times, want 2: the link must invalidate the cached team list", listCount)
	}
}
