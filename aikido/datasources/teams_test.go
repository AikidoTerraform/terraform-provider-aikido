package datasources

import (
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/teams"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func githubTeam() teams.Team {
	return teams.Team{
		ID: 1, Name: "Frontend developers", Active: true,
		ExternalSource: "github", ExternalSourceID: "gh-77",
		Responsibilities: []teams.Responsibility{
			{ID: 20, Type: teams.TypeCodeRepository},
			{ID: 10, Type: teams.TypeCodeRepository},
		},
	}
}

func manualTeam() teams.Team {
	return teams.Team{
		ID: 2, Name: "Security", Active: true,
		Responsibilities: []teams.Responsibility{
			{ID: 30, Type: teams.TypeCodeRepository},
			{ID: 40, Type: "cloud"},
		},
	}
}

func inactiveTeam() teams.Team {
	return teams.Team{ID: 3, Name: "Retired", Active: false}
}

func allTestTeams() []teams.Team {
	return []teams.Team{githubTeam(), manualTeam(), inactiveTeam()}
}

func matchedIDs(config teamsDataSourceModel) []int64 {
	_, ids := matchingTeams(allTestTeams(), config)

	plain := make([]int64, 0, len(ids))
	for _, id := range ids {
		plain = append(plain, id.ValueInt64())
	}

	return plain
}

func TestMatchingTeams_Filters(t *testing.T) {
	tests := []struct {
		name    string
		config  teamsDataSourceModel
		wantIDs []int64
	}{
		{
			name:    "no filter returns every team",
			config:  teamsDataSourceModel{},
			wantIDs: []int64{1, 2, 3},
		},
		{
			name:    "imported false selects teams created in Aikido",
			config:  teamsDataSourceModel{Imported: types.BoolValue(false)},
			wantIDs: []int64{2, 3},
		},
		{
			name:    "imported true selects synced teams",
			config:  teamsDataSourceModel{Imported: types.BoolValue(true)},
			wantIDs: []int64{1},
		},
		{
			name:    "external_source selects one provider",
			config:  teamsDataSourceModel{ExternalSource: types.StringValue("github")},
			wantIDs: []int64{1},
		},
		{
			name:    "external_source that matches nothing yields no teams",
			config:  teamsDataSourceModel{ExternalSource: types.StringValue("gitlab")},
			wantIDs: []int64{},
		},
		{
			name:    "name matches exactly",
			config:  teamsDataSourceModel{Name: types.StringValue("Security")},
			wantIDs: []int64{2},
		},
		{
			name:    "active narrows to live teams",
			config:  teamsDataSourceModel{Active: types.BoolValue(true)},
			wantIDs: []int64{1, 2},
		},
		{
			name: "filters combine with AND",
			config: teamsDataSourceModel{
				Active:   types.BoolValue(true),
				Imported: types.BoolValue(false),
			},
			wantIDs: []int64{2},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := matchedIDs(test.config)
			if len(got) != len(test.wantIDs) {
				t.Fatalf("ids = %v, want %v", got, test.wantIDs)
			}
			for i, wantID := range test.wantIDs {
				if got[i] != wantID {
					t.Errorf("ids = %v, want %v", got, test.wantIDs)
					break
				}
			}
		})
	}
}

func TestMatchingTeams_NoMatchYieldsEmptyNotNull(t *testing.T) {
	matched, ids := matchingTeams(allTestTeams(), teamsDataSourceModel{
		Name: types.StringValue("nothing called this"),
	})

	if matched == nil || ids == nil {
		t.Fatalf("teams = %v, ids = %v, want empty lists rather than null", matched, ids)
	}
	if len(matched) != 0 || len(ids) != 0 {
		t.Errorf("teams = %v, ids = %v, want both empty", matched, ids)
	}
}

func TestTeamModelFromAPI(t *testing.T) {
	t.Run("a synced team reports its source", func(t *testing.T) {
		model := teamModelFromAPI(githubTeam())

		if model.ID != types.StringValue("1") {
			t.Errorf("ID = %v, want \"1\" to match aikido_team", model.ID)
		}
		if model.Imported != types.BoolValue(true) {
			t.Errorf("Imported = %v, want true", model.Imported)
		}
		if model.ExternalSource != types.StringValue("github") || model.ExternalSourceID != types.StringValue("gh-77") {
			t.Errorf("source = %v/%v", model.ExternalSource, model.ExternalSourceID)
		}
	})

	t.Run("repository ids are sorted and code-only", func(t *testing.T) {
		if got := teamModelFromAPI(githubTeam()).RepositoryIDs; len(got) != 2 ||
			got[0] != types.Int64Value(10) || got[1] != types.Int64Value(20) {
			t.Errorf("RepositoryIDs = %v, want [10 20] in ascending order", got)
		}

		// The cloud responsibility has no place in repository_ids.
		if got := teamModelFromAPI(manualTeam()).RepositoryIDs; len(got) != 1 || got[0] != types.Int64Value(30) {
			t.Errorf("RepositoryIDs = %v, want just the code repository", got)
		}
	})

	t.Run("a team created in Aikido reports an empty source", func(t *testing.T) {
		model := teamModelFromAPI(manualTeam())

		if model.Imported != types.BoolValue(false) {
			t.Errorf("Imported = %v, want false", model.Imported)
		}
		if model.ExternalSource != types.StringValue("") {
			t.Errorf("ExternalSource = %v, want an empty string", model.ExternalSource)
		}
	})

	t.Run("a team with no responsibilities yields an empty list", func(t *testing.T) {
		if got := teamModelFromAPI(inactiveTeam()).RepositoryIDs; got == nil || len(got) != 0 {
			t.Errorf("RepositoryIDs = %v, want an empty list rather than null", got)
		}
	})
}

func TestUnknownTeamFilterDiagnostics(t *testing.T) {
	t.Run("known filters pass", func(t *testing.T) {
		diagnostics := unknownTeamFilterDiagnostics(teamsDataSourceModel{
			Name:     types.StringValue("Security"),
			Imported: types.BoolValue(false),
		})
		if diagnostics.HasError() {
			t.Errorf("got %v, want none", diagnostics)
		}
	})

	// An ignored filter would widen the result to every team, and that feeds team_id.
	t.Run("an unknown filter is refused", func(t *testing.T) {
		diagnostics := unknownTeamFilterDiagnostics(teamsDataSourceModel{
			Name: types.StringUnknown(),
		})
		if !diagnostics.HasError() {
			t.Fatal("unknown name filter produced no error")
		}
	})
}
