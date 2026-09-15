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

func int64Set(values ...int64) types.Set {
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.Int64Value(value))
	}

	return types.SetValueMust(types.Int64Type, elements)
}

func TestTeamModelFromAPI(t *testing.T) {
	team := teams.Team{
		ID:     42,
		Name:   "Payments",
		Active: true,
		Responsibilities: []teams.Responsibility{
			{ID: 9, Type: teams.TypeCodeRepository},
			{ID: 4, Type: teams.TypeCodeRepository},
			{ID: 3, Type: "cloud"},
		},
	}

	t.Run("managed repository ids are sorted and exclude non-code resources", func(t *testing.T) {
		model := teamModelFromAPI(team, true)

		if model.ID != types.StringValue("42") {
			t.Errorf("ID = %v, want string \"42\"", model.ID)
		}
		if model.Name != types.StringValue("Payments") {
			t.Errorf("Name = %v", model.Name)
		}
		if model.Active != types.BoolValue(true) {
			t.Errorf("Active = %v", model.Active)
		}

		want := int64Set(4, 9)
		if !model.RepositoryIDs.Equal(want) {
			t.Errorf("RepositoryIDs = %v, want %v", model.RepositoryIDs, want)
		}
	})

	// Omitted from config means unmanaged: adopting whatever the API returns
	// would show permanent drift against a config that never mentions them.
	t.Run("unmanaged repository ids stay null", func(t *testing.T) {
		model := teamModelFromAPI(team, false)

		if !model.RepositoryIDs.IsNull() {
			t.Errorf("RepositoryIDs = %v, want null", model.RepositoryIDs)
		}
	})
}

func TestRepositoryIDsFilter(t *testing.T) {
	ctx := context.Background()

	t.Run("null yields nil so responsibilities are left alone", func(t *testing.T) {
		ids, diagnostics := repositoryIDsFilter(ctx, types.SetNull(types.Int64Type))
		if diagnostics.HasError() {
			t.Fatalf("got %v", diagnostics)
		}
		if ids != nil {
			t.Errorf("ids = %v, want nil", ids)
		}
	})

	// An empty set is a real instruction — unlink everything — and must not be
	// confused with an absent one.
	t.Run("empty set yields a non-nil empty slice", func(t *testing.T) {
		ids, diagnostics := repositoryIDsFilter(ctx, int64Set())
		if diagnostics.HasError() {
			t.Fatalf("got %v", diagnostics)
		}
		if ids == nil {
			t.Fatal("ids = nil, want a non-nil empty slice")
		}
		if len(*ids) != 0 {
			t.Errorf("ids = %v, want empty", *ids)
		}
	})

	t.Run("values are converted", func(t *testing.T) {
		ids, diagnostics := repositoryIDsFilter(ctx, int64Set(4, 9))
		if diagnostics.HasError() {
			t.Fatalf("got %v", diagnostics)
		}
		if ids == nil || len(*ids) != 2 {
			t.Fatalf("ids = %v, want two entries", ids)
		}
	})
}

func TestTeamGuardDiagnostics(t *testing.T) {
	t.Run("a manual code-only team passes", func(t *testing.T) {
		team := teams.Team{ID: 1, Name: "Payments", Responsibilities: []teams.Responsibility{
			{ID: 2, Type: teams.TypeCodeRepository},
		}}

		if diagnostics := teamGuardDiagnostics(team, true); diagnostics.HasError() {
			t.Errorf("got %v, want none", diagnostics)
		}
	})

	t.Run("an imported team is rejected", func(t *testing.T) {
		team := teams.Team{ID: 1, Name: "Frontend developers", ExternalSource: "github"}

		diagnostics := teamGuardDiagnostics(team, false)
		if !diagnostics.HasError() {
			t.Fatal("imported team produced no error")
		}
		detail := diagnostics.Errors()[0].Detail()
		if !strings.Contains(detail, "github") || !strings.Contains(detail, "Frontend developers") {
			t.Errorf("detail %q should name the team and its source", detail)
		}
	})

	// A full replace cannot express these, so writing responsibilities would
	// destroy them. Refuse instead.
	t.Run("unrepresentable responsibilities are rejected only when managing them", func(t *testing.T) {
		team := teams.Team{ID: 1, Name: "Payments", Responsibilities: []teams.Responsibility{
			{ID: 2, Type: teams.TypeCodeRepository, IncludedPaths: []string{"/client"}},
			{ID: 3, Type: "cloud"},
		}}

		if diagnostics := teamGuardDiagnostics(team, false); diagnostics.HasError() {
			t.Errorf("got %v, want none when repository_ids is unmanaged", diagnostics)
		}

		diagnostics := teamGuardDiagnostics(team, true)
		if !diagnostics.HasError() {
			t.Fatal("unrepresentable responsibilities produced no error")
		}
		detail := diagnostics.Errors()[0].Detail()
		if !strings.Contains(detail, "cloud") || !strings.Contains(detail, "/client") {
			t.Errorf("detail %q should name what would be destroyed", detail)
		}
	})
}

// teamAPIServer answers the create, update and list calls, recording the order
// requests arrive in.
func teamAPIServer(t *testing.T, order *[]string, listed teams.Team) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*order = append(*order, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == teams.BasePath:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int64{"id": listed.ID})
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]teams.Team{listed})
		default:
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestCreateTeam(t *testing.T) {
	listed := teams.Team{ID: 42, Name: "Payments", Active: true, Responsibilities: []teams.Responsibility{
		{ID: 4, Type: teams.TypeCodeRepository},
	}}

	t.Run("creates then writes responsibilities", func(t *testing.T) {
		var order []string
		srv := teamAPIServer(t, &order, listed)

		res := &teamResource{client: testClient(srv)}
		state, diagnostics := res.createTeam(context.Background(), teamModel{
			Name:          types.StringValue("Payments"),
			RepositoryIDs: int64Set(4),
		})
		if diagnostics.HasError() {
			t.Fatalf("createTeam: %v", diagnostics)
		}

		if len(order) < 2 || order[0] != "POST "+teams.BasePath || order[1] != "PUT "+teams.BasePath+"/42" {
			t.Fatalf("call order = %v, want the create before the responsibilities write", order)
		}
		if state.ID != types.StringValue("42") {
			t.Errorf("ID = %v, want \"42\"", state.ID)
		}
		if !state.RepositoryIDs.Equal(int64Set(4)) {
			t.Errorf("RepositoryIDs = %v", state.RepositoryIDs)
		}
	})

	// Without repository_ids there is nothing to replace, and a PUT would be a
	// chance to destroy responsibilities the config never mentioned.
	t.Run("skips the responsibilities write when unmanaged", func(t *testing.T) {
		var order []string
		srv := teamAPIServer(t, &order, listed)

		res := &teamResource{client: testClient(srv)}
		state, diagnostics := res.createTeam(context.Background(), teamModel{
			Name:          types.StringValue("Payments"),
			RepositoryIDs: types.SetNull(types.Int64Type),
		})
		if diagnostics.HasError() {
			t.Fatalf("createTeam: %v", diagnostics)
		}

		for _, call := range order {
			if strings.HasPrefix(call, http.MethodPut) {
				t.Errorf("call order = %v, want no PUT", order)
			}
		}
		if !state.RepositoryIDs.IsNull() {
			t.Errorf("RepositoryIDs = %v, want null", state.RepositoryIDs)
		}
	})
}

// A team that was created but could not be fully configured still exists in
// Aikido. Returning no state would orphan it: Terraform would create another one
// on the next apply and never manage the first.
func TestCreateTeam_KeepsTheIDWhenLaterCallsFail(t *testing.T) {
	t.Run("responsibilities write fails", func(t *testing.T) {
		var order []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, r.Method+" "+r.URL.Path)

			switch r.Method {
			case http.MethodPost:
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]int64{"id": 42})
			case http.MethodPut:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"reason_phrase":"unknown repository"}`)
			default:
				_ = json.NewEncoder(w).Encode([]teams.Team{{ID: 42, Name: "Payments"}})
			}
		}))
		t.Cleanup(srv.Close)

		res := &teamResource{client: testClient(srv)}
		state, diagnostics := res.createTeam(context.Background(), teamModel{
			Name:          types.StringValue("Payments"),
			RepositoryIDs: int64Set(4),
		})

		if !diagnostics.HasError() {
			t.Fatal("failed responsibilities write produced no error")
		}
		if state.ID != types.StringValue("42") {
			t.Errorf("ID = %v, want \"42\" so the created team stays tracked", state.ID)
		}
		// The team must not be deleted: the write may have partially applied, and
		// a later apply converges on it.
		for _, call := range order {
			if strings.HasPrefix(call, http.MethodDelete) {
				t.Errorf("call order = %v, want no rollback delete", order)
			}
		}
	})

	t.Run("read back fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]int64{"id": 42})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		res := &teamResource{client: testClient(srv)}
		state, diagnostics := res.createTeam(context.Background(), teamModel{
			Name:          types.StringValue("Payments"),
			RepositoryIDs: types.SetNull(types.Int64Type),
		})

		if !diagnostics.HasError() {
			t.Fatal("failed read back produced no error")
		}
		if state.ID != types.StringValue("42") {
			t.Errorf("ID = %v, want \"42\" so the created team stays tracked", state.ID)
		}
		if state.Name != types.StringValue("Payments") {
			t.Errorf("Name = %v, want the planned name", state.Name)
		}
	})
}

func TestUpdateTeam_RefusesToDestroyUnrepresentableResponsibilities(t *testing.T) {
	var order []string
	srv := teamAPIServer(t, &order, teams.Team{ID: 42, Name: "Payments", Responsibilities: []teams.Responsibility{
		{ID: 3, Type: "cloud"},
	}})

	res := &teamResource{client: testClient(srv)}
	_, diagnostics := res.updateTeam(context.Background(), 42, teamModel{
		ID:            types.StringValue("42"),
		Name:          types.StringValue("Payments"),
		RepositoryIDs: int64Set(4),
	})

	if !diagnostics.HasError() {
		t.Fatal("update against a team with a cloud responsibility produced no error")
	}
	for _, call := range order {
		if strings.HasPrefix(call, http.MethodPut) {
			t.Errorf("call order = %v, want the write refused before it is sent", order)
		}
	}
}
