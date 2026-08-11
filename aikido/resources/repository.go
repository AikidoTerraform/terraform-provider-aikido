package resources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	basePath             = "/public/v1/repositories/code"
	repositoriesPageSize = 200
	repositoriesCacheKey = "repositories/code"
)

var (
	_ resource.Resource                = &repositoryResource{}
	_ resource.ResourceWithImportState = &repositoryResource{}
	_ resource.ResourceWithConfigure   = &repositoryResource{}
)

func NewRepositoryResource() resource.Resource {
	return &repositoryResource{}
}

type repositoryResource struct {
	client *client.Client
}

// repositoryModel is the Terraform state. IDs are strings by TF convention even
// though the API uses integers.
type repositoryModel struct {
	ID             types.String   `tfsdk:"id"`
	Active         types.Bool     `tfsdk:"active"`
	Sensitivity    types.String   `tfsdk:"sensitivity"`
	Connectivity   types.String   `tfsdk:"connectivity"`
	Name           types.String   `tfsdk:"name"`
	GitProvider    types.String   `tfsdk:"git_provider"`
	Branch         types.String   `tfsdk:"branch"`
	URL            types.String   `tfsdk:"url"`
	ExternalRepoID types.String   `tfsdk:"external_repo_id"`
	Labels         []types.String `tfsdk:"labels"`
}

type repositoryAPI struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Provider       string     `json:"provider"`
	ExternalRepoID string     `json:"external_repo_id"`
	Active         bool       `json:"active"`
	Branch         string     `json:"branch"`
	URL            string     `json:"url"`
	Connectivity   string     `json:"connectivity"`
	Sensitivity    string     `json:"sensitivity"`
	Labels         []labelAPI `json:"labels"`
}

// Metadata sets the resource type name.
func (r *repositoryResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_repository"
}

// Schema defines the full resource shape: user-settable fields and computed-only fields
// populated from the API (name, git_provider, etc.). Computed attributes cannot be set in .tf
// but must be declared so the provider can store them in state and expose them in show/outputs.
func (r *repositoryResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages activation and configuration of an existing Aikido code repository. " +
			"The repository must already exist in Aikido (synced from a Git provider); this resource never creates or deletes the repo. " +
			"Apply sets active and optional config; destroy deactivates it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Aikido code repository ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"active": schema.BoolAttribute{
				Required:    true,
				Description: "Whether the repository is activated for scanning in Aikido.",
			},
			"sensitivity": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Sensitivity level of the repository. One of: extreme, sensitive, normal, not_sensitive, no_data.",
				Validators: []validator.String{
					stringvalidator.OneOf("extreme", "sensitive", "normal", "not_sensitive", "no_data"),
				},
			},
			"connectivity": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the code runs on an internet-connected server. One of: connected, not_connected, unknown.",
				Validators: []validator.String{
					stringvalidator.OneOf("connected", "not_connected", "unknown"),
				},
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the code repository.",
			},
			"git_provider": schema.StringAttribute{
				Computed:    true,
				Description: "Git provider hosting the repository (e.g. github, gitlab, bitbucket).",
			},
			"branch": schema.StringAttribute{
				Computed:    true,
				Description: "Branch configured for scanning.",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "External URL of the repository.",
			},
			"external_repo_id": schema.StringAttribute{
				Computed:    true,
				Description: "Repository ID from the Git provider.",
			},
			"labels": labelsSchemaAttribute(),
		},
	}
}

func (r *repositoryResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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
	r.client = apiClient
}

// Create is called on first apply when the resource is in config but not yet in state.
// It activates/deactivates and optionally configures an existing Aikido repo.
func (r *repositoryResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var plannedRepository repositoryModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &plannedRepository)...)
	if response.Diagnostics.HasError() {
		return
	}

	repositoryState, err := r.setRepoConfig(ctx, plannedRepository)
	if err != nil {
		response.Diagnostics.AddError("Error configuring repository", err.Error())
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, repositoryState)...)
}

// Read is called during refresh/plan to sync the repository from the API into state.
func (r *repositoryResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState repositoryModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseRepositoryID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading repository", err.Error())
		return
	}

	// get repository from list cache
	apiRepository, err := repositoryFromCache(ctx, r.client, id)
	if err != nil {
		if client.NotFound(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading repository", err.Error())
		return
	}

	updatedState := repositoryModelFromAPI(apiRepository)
	// Labels omitted from config are unmanaged — don't import API labels into state.
	if priorState.Labels == nil {
		updatedState.Labels = nil
	}
	response.Diagnostics.Append(response.State.Set(ctx, updatedState)...)
}

// Update is called on apply when config changes in-place (no replacement).
// It applies the new config to the existing Aikido repo.
func (r *repositoryResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var plannedRepository repositoryModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &plannedRepository)...)
	if response.Diagnostics.HasError() {
		return
	}

	repositoryState, err := r.setRepoConfig(ctx, plannedRepository)
	if err != nil {
		response.Diagnostics.AddError("Error configuring repository", err.Error())
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, repositoryState)...)
}

// Delete is called when the resource is removed from config or on destroy.
// Deactivates the repo.
func (r *repositoryResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState repositoryModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	if err := r.setActive(ctx, priorState.ID.ValueString(), false); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deactivating repository", err.Error())
	}
}

// ImportState lets users adopt an existing Aikido repo into Terraform state by ID.
func (r *repositoryResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

// setRepoConfig is shared by Create and Update.
func (r *repositoryResource) setRepoConfig(ctx context.Context, plannedRepository repositoryModel) (repositoryModel, error) {
	repositoryID := plannedRepository.ID.ValueString()

	if err := r.setActive(ctx, repositoryID, plannedRepository.Active.ValueBool()); err != nil {
		return repositoryModel{}, err
	}
	if !plannedRepository.Sensitivity.IsNull() && !plannedRepository.Sensitivity.IsUnknown() {
		sensitivityBody := map[string]string{"sensitivity": plannedRepository.Sensitivity.ValueString()}
		if err := r.client.Do(ctx, "PUT", basePath+"/"+repositoryID+"/sensitivity", sensitivityBody, nil); err != nil {
			return repositoryModel{}, fmt.Errorf("updating sensitivity: %w", err)
		}
	}
	if !plannedRepository.Connectivity.IsNull() && !plannedRepository.Connectivity.IsUnknown() {
		connectivityBody := map[string]string{"connectivity": plannedRepository.Connectivity.ValueString()}
		if err := r.client.Do(ctx, "PUT", basePath+"/"+repositoryID+"/connectivity", connectivityBody, nil); err != nil {
			return repositoryModel{}, fmt.Errorf("updating connectivity: %w", err)
		}
	}

	// Detail GET after writes: label IDs for applyLabels + computed fields for state.
	id, err := parseRepositoryID(repositoryID)
	if err != nil {
		return repositoryModel{}, err
	}

	apiRepository, err := repositoryFromAPI(ctx, r.client, id)
	if err != nil {
		return repositoryModel{}, err
	}

	if err := r.applyLabels(ctx, repositoryID, plannedRepository.Labels, apiRepository.Labels); err != nil {
		return repositoryModel{}, err
	}

	repositoryState := repositoryModelFromAPI(apiRepository)
	repositoryState.Labels = plannedRepository.Labels

	return repositoryState, nil
}

// setActive activates or deactivates the repository.
func (r *repositoryResource) setActive(ctx context.Context, repositoryID string, isActive bool) error {
	codeRepoID, err := parseRepositoryID(repositoryID)
	if err != nil {
		return err
	}

	endpoint := basePath + "/deactivate"
	if isActive {
		endpoint = basePath + "/activate"
	}
	return r.client.Do(ctx, "POST", endpoint, map[string]int64{"code_repo_id": codeRepoID}, nil)
}

func parseRepositoryID(repositoryID string) (int64, error) {
	id, err := strconv.ParseInt(repositoryID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid repository id %q: %w", repositoryID, err)
	}

	return id, nil
}

// repositoryFromCache looks up a repo in the shared paginated list cache.
// Use for Read when many resources share one plan (avoids N detail GETs).
func repositoryFromCache(ctx context.Context, c *client.Client, id int64) (repositoryAPI, error) {
	repos, err := client.LoadCached(c, ctx, repositoriesCacheKey, func(ctx context.Context) (map[int64]repositoryAPI, error) {
		// fetch all repositories from the API once on first use
		items, err := client.FetchAllPages[repositoryAPI](
			ctx, c, basePath, repositoriesPageSize,
			"include_inactive=true&include_labels=true",
		)
		if err != nil {
			return nil, err
		}

		repoCacheMap := make(map[int64]repositoryAPI, len(items))
		for _, repo := range items {
			repoCacheMap[repo.ID] = repo
		}

		return repoCacheMap, nil
	})

	if err != nil {
		return repositoryAPI{}, err
	}

	// lookup the repository in the cache
	cachedRepo, ok := repos[id]
	if !ok {
		return repositoryAPI{}, &client.APIError{
			StatusCode: http.StatusNotFound,
			Method:     http.MethodGet,
			Path:       basePath + "/" + strconv.FormatInt(id, 10),
			Body:       "repository not found",
		}
	}

	return cachedRepo, nil
}

// repositoryFromAPI loads one repository via GET /repositories/code/{id}.
// Use after writes so state reflects the API rather than a possibly stale list cache.
func repositoryFromAPI(ctx context.Context, c *client.Client, id int64) (repositoryAPI, error) {
	var repo repositoryAPI
	path := basePath + "/" + strconv.FormatInt(id, 10)

	if err := c.Do(ctx, http.MethodGet, path, nil, &repo); err != nil {
		return repositoryAPI{}, err
	}

	return repo, nil
}

// repositoryModelFromAPI maps an API repository into a Terraform state model.
func repositoryModelFromAPI(apiRepository repositoryAPI) repositoryModel {
	repositoryState := repositoryModel{
		ID:             types.StringValue(strconv.FormatInt(apiRepository.ID, 10)),
		Active:         types.BoolValue(apiRepository.Active),
		Name:           types.StringValue(apiRepository.Name),
		GitProvider:    types.StringValue(apiRepository.Provider),
		Branch:         types.StringValue(apiRepository.Branch),
		URL:            types.StringValue(apiRepository.URL),
		ExternalRepoID: types.StringValue(apiRepository.ExternalRepoID),
		Labels:         labelNamesFromAPI(apiRepository.Labels),
	}
	if apiRepository.Sensitivity != "" {
		repositoryState.Sensitivity = types.StringValue(apiRepository.Sensitivity)
	} else {
		repositoryState.Sensitivity = types.StringNull()
	}
	if apiRepository.Connectivity != "" {
		repositoryState.Connectivity = types.StringValue(apiRepository.Connectivity)
	} else {
		repositoryState.Connectivity = types.StringNull()
	}
	return repositoryState
}

// labelNamesFromAPI maps API labels into Terraform state (names only).
// Always returns a non-nil slice so a managed empty set differs from omitted (nil).
func labelNamesFromAPI(apiLabels []labelAPI) []types.String {
	names := make([]types.String, 0, len(apiLabels))
	for _, apiLabel := range apiLabels {
		names = append(names, types.StringValue(apiLabel.Name))
	}

	return names
}
