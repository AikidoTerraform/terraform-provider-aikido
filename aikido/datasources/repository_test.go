package datasources

import (
	"context"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestRepositorySchema_IsValid(t *testing.T) {
	ctx := context.Background()
	response := &datasource.SchemaResponse{}

	NewRepositoryDataSource().Schema(ctx, datasource.SchemaRequest{}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", response.Diagnostics)
	}
	if diagnostics := response.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
		t.Fatalf("schema implementation: %v", diagnostics)
	}

	// The state model is shared with aikido_repositories, so every attribute
	// the model carries must exist in this schema and vice versa, or State.Set
	// fails at read time.
	if _, ok := response.Schema.Attributes["name"]; !ok {
		t.Error("schema has no name attribute")
	}
	if len(response.Schema.Attributes) != 12 {
		t.Errorf("schema has %d attributes, want the 12 fields of repositoryModel", len(response.Schema.Attributes))
	}
}

func TestRepositoryMetadata_TypeName(t *testing.T) {
	response := &datasource.MetadataResponse{}

	NewRepositoryDataSource().Metadata(
		context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "aikido"},
		response,
	)

	if response.TypeName != "aikido_repository" {
		t.Errorf("TypeName = %q, want aikido_repository", response.TypeName)
	}
}

func TestSingleRepository_OneMatchIsReturned(t *testing.T) {
	want := repositories.Repository{ID: 10, Name: "payments", Branch: "main"}

	got, diagnostics := singleRepository("payments", []repositories.Repository{want})
	if diagnostics.HasError() {
		t.Fatalf("got %v, want no diagnostics", diagnostics)
	}
	if got.ID != want.ID {
		t.Errorf("repository = %#v, want %#v", got, want)
	}
}

// An inactive repository is still exactly one repository; activation state is
// reported, not filtered on.
func TestSingleRepository_InactiveMatchIsReturned(t *testing.T) {
	want := repositories.Repository{ID: 10, Name: "payments", Active: false}

	got, diagnostics := singleRepository("payments", []repositories.Repository{want})
	if diagnostics.HasError() {
		t.Fatalf("got %v, want no diagnostics", diagnostics)
	}
	if got.ID != want.ID || got.Active {
		t.Errorf("repository = %#v, want the inactive repository %d", got, want.ID)
	}
}

// A single-repository lookup must fail rather than return an empty repository,
// since downstream resources would otherwise receive an empty ID.
func TestSingleRepository_NoMatchIsAnError(t *testing.T) {
	_, diagnostics := singleRepository("payments", nil)

	if !diagnostics.HasError() {
		t.Fatal("no match produced no error")
	}
	if summary := diagnostics.Errors()[0].Summary(); summary != "Repository not found" {
		t.Errorf("summary = %q", summary)
	}
	if detail := diagnostics.Errors()[0].Detail(); !strings.Contains(detail, `"payments"`) {
		t.Errorf("detail %q does not name the repository", detail)
	}
}

// A cloned (multi-branch) repository appears as several repositories with the
// same name. Picking one silently would feed an arbitrary clone's ID downstream,
// so the lookup refuses and points at the branch-aware data source.
func TestSingleRepository_ClonedRepositoryIsAnError(t *testing.T) {
	clones := []repositories.Repository{
		{ID: 10, Name: "payments", Provider: "github", Branch: "main"},
		{ID: 30, Name: "payments", Provider: "github", Branch: "develop"},
	}

	_, diagnostics := singleRepository("payments", clones)

	if !diagnostics.HasError() {
		t.Fatal("two clones produced no error")
	}
	if summary := diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "cloned") {
		t.Errorf("summary = %q, want it to explain the repository is cloned", summary)
	}

	detail := diagnostics.Errors()[0].Detail()
	for _, want := range []string{"2 code repositories", `"payments"`, "ID 10", "ID 30", `branch "main"`, `branch "develop"`, "aikido_repositories"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q missing %q", detail, want)
		}
	}
}
