package datasources

import (
	"context"
	"fmt"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &repositoryDataSource{}
	_ datasource.DataSourceWithConfigure = &repositoryDataSource{}
)

func NewRepositoryDataSource() datasource.DataSource {
	return &repositoryDataSource{}
}

type repositoryDataSource struct {
	client *client.Client
}

// repositoryDataSourceModel is the state of a single-repository lookup. It is the
// same shape as one entry of aikido_repositories, with name doubling as the
// required input, so both data sources map API repositories through
// repositoryModelFromAPI and expose identical attributes.
type repositoryDataSourceModel = repositoryModel

func (d *repositoryDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_repository"
}

func (d *repositoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Looks up exactly one Aikido code repository by name and, optionally, branch. " +
			"It is an error when no repository matches or when several repositories match. " +
			"Set branch when the same repository is configured for multiple branches.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				Description: "Name of the code repository, matched exactly. " +
					"Matching is exact, not a substring or glob. The name and optional branch must identify exactly one repository, active or inactive.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido code repository ID, as a string to match the id attribute of aikido_repository. Use tonumber(...) to feed the numeric code_repo_id and repo_ids attributes of the other resources.",
			},
			"git_provider": schema.StringAttribute{
				Computed:    true,
				Description: "Git provider hosting the repository.",
			},
			"external_repo_id": schema.StringAttribute{
				Computed:    true,
				Description: "Repository ID from the Git provider.",
			},
			"external_repo_numeric_id": schema.Int64Attribute{
				Computed:    true,
				Description: "Numeric repository ID from the Git provider.",
			},
			"active": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the repository is activated for scanning in Aikido.",
			},
			"branch": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Branch configured for scanning. " +
					"Set this to select one repository when several repositories have the same name.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "External URL of the repository.",
			},
			"last_scanned_at": schema.Int64Attribute{
				Computed:    true,
				Description: "Unix timestamp of the last completed scan, or -1 if the repository has never been scanned.",
			},
			"connectivity": schema.StringAttribute{
				Computed:    true,
				Description: "Whether the code runs on an internet-connected server. One of: connected, not_connected, unknown.",
			},
			"sensitivity": schema.StringAttribute{
				Computed:    true,
				Description: "Sensitivity level of the repository. One of: extreme, sensitive, normal, not_sensitive, no_data.",
			},
			"labels": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Label names on the repository, sorted alphabetically.",
			},
		},
	}
}

func (d *repositoryDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
	if request.ProviderData == nil {
		return
	}
	apiClient, isClient := request.ProviderData.(*client.Client)
	if !isClient {
		response.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", request.ProviderData),
		)
		return
	}
	d.client = apiClient
}

func (d *repositoryDataSource) Read(ctx context.Context, request datasource.ReadRequest, response *datasource.ReadResponse) {
	var config repositoryDataSourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}

	// Terraform defers a data source read until apply when its configuration
	// is unknown, so this only guards against a framework change. Refusing beats
	// looking up an empty name, which would select by the server's idea of a
	// match rather than the user's.
	if config.Name.IsUnknown() {
		response.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Unknown repository name",
			"The name is not known at read time, so it cannot be used to select a repository. "+
				"Set it to a value that does not depend on an attribute that is only known after apply.",
		)
		return
	}
	name := config.Name.ValueString()
	if config.Branch.IsUnknown() {
		response.Diagnostics.AddAttributeError(
			path.Root("branch"),
			"Unknown repository branch",
			"The branch is not known at read time, so it cannot be used to select a repository. "+
				"Set it to a value that does not depend on an attribute that is only known after apply.",
		)
		return
	}
	branch := ""
	if !config.Branch.IsNull() {
		branch = config.Branch.ValueString()
	}

	matched, err := repositories.ByName(ctx, d.client, name, branch)
	if err != nil {
		response.Diagnostics.AddError("Error reading repository", err.Error())
		return
	}

	repository, diagnostics := singleRepository(name, branch, matched)
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}

	state := repositoryModelFromAPI(repository)
	response.Diagnostics.Append(response.State.Set(ctx, &state)...)
}

// singleRepository reduces the exact-name matches to the one repository the
// data source stands for. No match and several matches are both errors, because
// a single-repository lookup that silently picked one of several clones would
// feed the wrong ID downstream.
func singleRepository(name, branch string, matched []repositories.Repository) (repositories.Repository, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		target := fmt.Sprintf("named %q", name)
		if branch != "" {
			target += fmt.Sprintf(" on branch %q", branch)
		}
		diagnostics.AddAttributeError(
			path.Root("name"),
			"Repository not found",
			fmt.Sprintf("No code repository %s exists in the Aikido workspace, active or inactive. "+
				"The filters are matched exactly, so check their case and spelling against the repository list in Aikido.", target),
		)
	default:
		target := fmt.Sprintf("named %q", name)
		guidance := "Set branch to select one of them."
		if branch != "" {
			target += fmt.Sprintf(" on branch %q", branch)
			guidance = "Use one of the listed Aikido repository IDs to identify the intended repository."
		}
		diagnostics.AddAttributeError(
			path.Root("name"),
			"Repository lookup is ambiguous",
			fmt.Sprintf("%d code repositories are %s: %s. %s",
				len(matched), target, describeRepositories(matched), guidance),
		)
	}

	return repositories.Repository{}, diagnostics
}

// describeRepositories renders the matches so the error names which clones exist
// and how to tell them apart.
func describeRepositories(matched []repositories.Repository) string {
	descriptions := make([]string, 0, len(matched))
	for _, repository := range matched {
		descriptions = append(descriptions, fmt.Sprintf("ID %d (%s, branch %q)", repository.ID, repository.Provider, repository.Branch))
	}

	return strings.Join(descriptions, ", ")
}
