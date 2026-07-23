// Package repository implements the aikido_repository resource.
package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const basePath = "/public/v1/repositories/code"

var (
	_ resource.Resource                = &repositoryResource{}
	_ resource.ResourceWithImportState = &repositoryResource{}
	_ resource.ResourceWithConfigure   = &repositoryResource{}
)

func NewResource() resource.Resource {
	return &repositoryResource{}
}

type repositoryResource struct {
	client *client.Client
}

// repositoryModel is the Terraform state. IDs are strings by TF convention even
// though the API uses integers.
type repositoryModel struct {
	ID             types.String `tfsdk:"id"`
	Active         types.Bool   `tfsdk:"active"`
	Sensitivity    types.String `tfsdk:"sensitivity"`
	Connectivity   types.String `tfsdk:"connectivity"`
	Name           types.String `tfsdk:"name"`
	GitProvider    types.String `tfsdk:"git_provider"`
	Branch         types.String `tfsdk:"branch"`
	URL            types.String `tfsdk:"url"`
	ExternalRepoID types.String `tfsdk:"external_repo_id"`
	Labels         []labelModel `tfsdk:"labels"`
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
			},
			"connectivity": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the code runs on an internet-connected server. One of: connected, not_connected, unknown.",
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

	repositoryState, err := r.setRepoConfig(ctx, plannedRepository, nil)
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

	updatedState, err := r.getRepositoryDetails(ctx, priorState.ID.ValueString())
	if err != nil {
		if client.NotFound(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading repository", err.Error())
		return
	}
	// Labels omitted from config are unmanaged — don't import API labels into state.
	if priorState.Labels == nil {
		updatedState.Labels = nil
	}
	response.Diagnostics.Append(response.State.Set(ctx, updatedState)...)
}

// Update is called on apply when config changes in-place (no replacement).
// It applies the new config to the existing Aikido repo.
func (r *repositoryResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var plannedRepository, priorState repositoryModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &plannedRepository)...)
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	repositoryState, err := r.setRepoConfig(ctx, plannedRepository, priorState.Labels)
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
func (r *repositoryResource) setRepoConfig(ctx context.Context, plannedRepository repositoryModel, priorLabels []labelModel) (repositoryModel, error) {
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

	syncedLabels, err := r.applyLabels(ctx, repositoryID, plannedRepository.Labels, priorLabels)
	if err != nil {
		return repositoryModel{}, err
	}

	repositoryState, err := r.getRepositoryDetails(ctx, repositoryID)
	if err != nil {
		return repositoryModel{}, err
	}
	repositoryState.Labels = syncedLabels
	return repositoryState, nil
}

// setActive activates or deactivates the repository.
func (r *repositoryResource) setActive(ctx context.Context, repositoryID string, isActive bool) error {
	codeRepoID, err := strconv.ParseInt(repositoryID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid repository id %q: %w", repositoryID, err)
	}

	endpoint := basePath + "/deactivate"
	if isActive {
		endpoint = basePath + "/activate"
	}
	return r.client.Do(ctx, "POST", endpoint, map[string]int64{"code_repo_id": codeRepoID}, nil)
}

// read reads the repository from the API into the state.
func (r *repositoryResource) getRepositoryDetails(ctx context.Context, repositoryID string) (repositoryModel, error) {
	var apiRepository repositoryAPI
	if err := r.client.Do(ctx, "GET", basePath+"/"+repositoryID, nil, &apiRepository); err != nil {
		return repositoryModel{}, err
	}

	repositoryState := repositoryModel{
		ID:             types.StringValue(strconv.FormatInt(apiRepository.ID, 10)),
		Active:         types.BoolValue(apiRepository.Active),
		Name:           types.StringValue(apiRepository.Name),
		GitProvider:    types.StringValue(apiRepository.Provider),
		Branch:         types.StringValue(apiRepository.Branch),
		URL:            types.StringValue(apiRepository.URL),
		ExternalRepoID: types.StringValue(apiRepository.ExternalRepoID),
		Labels:         labelModelsFromAPI(apiRepository.Labels),
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
	return repositoryState, nil
}
