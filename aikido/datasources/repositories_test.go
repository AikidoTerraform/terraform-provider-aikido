package datasources

import (
	"strconv"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMatchesFilters(t *testing.T) {
	repo := repositories.Repository{
		ID:       1,
		Name:     "payments",
		Provider: "github",
		Branch:   "main",
		Active:   true,
	}

	tests := []struct {
		name   string
		config repositoriesDataSourceModel
		want   bool
	}{
		{
			name:   "no filters matches everything",
			config: repositoriesDataSourceModel{},
			want:   true,
		},
		{
			name:   "name matches exactly",
			config: repositoriesDataSourceModel{Name: types.StringValue("payments")},
			want:   true,
		},
		{
			name:   "name is not a substring match",
			config: repositoriesDataSourceModel{Name: types.StringValue("pay")},
			want:   false,
		},
		{
			name:   "branch mismatch excludes",
			config: repositoriesDataSourceModel{Branch: types.StringValue("develop")},
			want:   false,
		},
		{
			name:   "git provider matches",
			config: repositoriesDataSourceModel{GitProvider: types.StringValue("github")},
			want:   true,
		},
		{
			name:   "active false excludes an active repository",
			config: repositoriesDataSourceModel{Active: types.BoolValue(false)},
			want:   false,
		},
		{
			name: "filters combine with AND",
			config: repositoriesDataSourceModel{
				Name:        types.StringValue("payments"),
				GitProvider: types.StringValue("gitlab"),
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesFilters(repo, test.config); got != test.want {
				t.Errorf("matchesFilters = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRepositoryModelFromAPI(t *testing.T) {
	model := repositoryModelFromAPI(repositories.Repository{
		ID:                    42,
		Name:                  "payments",
		Provider:              "github",
		ExternalRepoID:        "R_abc",
		ExternalRepoNumericID: 7788,
		Active:                true,
		Branch:                "main",
		URL:                   "https://github.com/acme/payments",
		LastScannedAt:         1735689600,
		Connectivity:          "connected",
		Sensitivity:           "sensitive",
		Labels: []repositories.Label{
			{ID: "2", Name: "tier-1"},
			{ID: "1", Name: "backend"},
		},
	})

	if model.ID != types.StringValue("42") {
		t.Errorf("ID = %v, want string \"42\"", model.ID)
	}
	if model.ExternalRepoNumericID != types.Int64Value(7788) {
		t.Errorf("ExternalRepoNumericID = %v", model.ExternalRepoNumericID)
	}
	if model.LastScannedAt != types.Int64Value(1735689600) {
		t.Errorf("LastScannedAt = %v", model.LastScannedAt)
	}
	if model.GitProvider != types.StringValue("github") {
		t.Errorf("GitProvider = %v", model.GitProvider)
	}

	wantLabels := []string{"backend", "tier-1"}
	if len(model.Labels) != len(wantLabels) {
		t.Fatalf("got %d labels, want %d", len(model.Labels), len(wantLabels))
	}
	for i, want := range wantLabels {
		if model.Labels[i] != types.StringValue(want) {
			t.Errorf("label %d = %v, want %q", i, model.Labels[i], want)
		}
	}
}

func TestRepositoryModelFromAPI_EmptyEnumsBecomeNull(t *testing.T) {
	model := repositoryModelFromAPI(repositories.Repository{ID: 1})

	if !model.Connectivity.IsNull() {
		t.Errorf("Connectivity = %v, want null", model.Connectivity)
	}
	if !model.Sensitivity.IsNull() {
		t.Errorf("Sensitivity = %v, want null", model.Sensitivity)
	}
}

// workspace mirrors the shape the complaint describes: repositories across
// providers, active and inactive, sharing a name across two providers.
var workspace = []repositories.Repository{
	{ID: 10, Name: "payments", Provider: "github", Branch: "main", Active: true},
	{ID: 20, Name: "legacy-batch", Provider: "github", Branch: "master", Active: false},
	{ID: 30, Name: "checkout", Provider: "gitlab", Branch: "main", Active: true},
	{ID: 40, Name: "payments", Provider: "gitlab", Branch: "main", Active: true},
}

// ids must line up with repositories entry for entry after filtering, because
// repo_ids (Set of Number) and code_repo_id (Int64) are fed straight from ids.
func TestMatchingRepositories_IDsStayAlignedAfterFiltering(t *testing.T) {
	tests := []struct {
		name    string
		config  repositoriesDataSourceModel
		wantIDs []int64
	}{
		{
			name:    "no filters returns everything",
			config:  repositoriesDataSourceModel{},
			wantIDs: []int64{10, 20, 30, 40},
		},
		{
			name:    "git provider filter",
			config:  repositoriesDataSourceModel{GitProvider: types.StringValue("github")},
			wantIDs: []int64{10, 20},
		},
		{
			name: "provider and active combine with AND",
			config: repositoriesDataSourceModel{
				GitProvider: types.StringValue("github"),
				Active:      types.BoolValue(true),
			},
			wantIDs: []int64{10},
		},
		{
			name:    "a name shared across providers returns both",
			config:  repositoriesDataSourceModel{Name: types.StringValue("payments")},
			wantIDs: []int64{10, 40},
		},
		{
			name:    "name and git_provider disambiguate a shared name",
			config:  repositoriesDataSourceModel{Name: types.StringValue("payments"), GitProvider: types.StringValue("gitlab")},
			wantIDs: []int64{40},
		},
		{
			name:    "no match yields empty, not null",
			config:  repositoriesDataSourceModel{Name: types.StringValue("does-not-exist")},
			wantIDs: []int64{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matched, matchedIDs := matchingRepositories(workspace, test.config)

			if matched == nil || matchedIDs == nil {
				t.Fatal("matchingRepositories returned nil; want non-nil slices so Terraform sees an empty list")
			}
			if len(matched) != len(matchedIDs) {
				t.Fatalf("%d repositories but %d ids", len(matched), len(matchedIDs))
			}
			if len(matchedIDs) != len(test.wantIDs) {
				t.Fatalf("got %d matches, want %d", len(matchedIDs), len(test.wantIDs))
			}

			for i, wantID := range test.wantIDs {
				if matchedIDs[i].ValueInt64() != wantID {
					t.Errorf("ids[%d] = %d, want %d", i, matchedIDs[i].ValueInt64(), wantID)
				}
				// The numeric ids entry and the string repositories entry must
				// describe the same repository at the same position.
				if matched[i].ID.ValueString() != strconv.FormatInt(wantID, 10) {
					t.Errorf("repositories[%d].id = %s, want %d", i, matched[i].ID.ValueString(), wantID)
				}
			}
		})
	}
}

func TestUnknownFilterDiagnostics(t *testing.T) {
	t.Run("known filters produce no error", func(t *testing.T) {
		config := repositoriesDataSourceModel{
			Name:        types.StringValue("payments"),
			GitProvider: types.StringValue("github"),
		}

		if diagnostics := unknownFilterDiagnostics(config); diagnostics.HasError() {
			t.Errorf("got %v, want no diagnostics", diagnostics)
		}
	})

	t.Run("null filters produce no error", func(t *testing.T) {
		if diagnostics := unknownFilterDiagnostics(repositoriesDataSourceModel{}); diagnostics.HasError() {
			t.Errorf("got %v, want no diagnostics", diagnostics)
		}
	})

	// An unknown filter must fail rather than be ignored: ignoring it would
	// return every repository, which then feeds repo_ids.
	t.Run("each unknown filter is reported", func(t *testing.T) {
		tests := []struct {
			name   string
			config repositoriesDataSourceModel
		}{
			{"name", repositoriesDataSourceModel{Name: types.StringUnknown()}},
			{"branch", repositoriesDataSourceModel{Branch: types.StringUnknown()}},
			{"git_provider", repositoriesDataSourceModel{GitProvider: types.StringUnknown()}},
			{"active", repositoriesDataSourceModel{Active: types.BoolUnknown()}},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				diagnostics := unknownFilterDiagnostics(test.config)
				if !diagnostics.HasError() {
					t.Fatalf("unknown %s produced no error", test.name)
				}
				if !strings.Contains(diagnostics.Errors()[0].Detail(), test.name) {
					t.Errorf("error detail %q does not name the %s filter",
						diagnostics.Errors()[0].Detail(), test.name)
				}
			})
		}
	})
}

func TestSortedLabelNames_EmptyIsNonNil(t *testing.T) {
	if labels := sortedLabelNames(nil); labels == nil {
		t.Error("sortedLabelNames(nil) = nil, want an empty non-nil slice")
	}
}
