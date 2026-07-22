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
}

type repositoryAPI struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	ExternalRepoID string `json:"external_repo_id"`
	Active         bool   `json:"active"`
	Branch         string `json:"branch"`
	URL            string `json:"url"`
	Connectivity   string `json:"connectivity"`
	Sensitivity    string `json:"sensitivity"`
}

// Metadata sets the resource type name.
func (r *repositoryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository"
}

// Schema defines the full resource shape: user-settable fields and computed-only fields
// populated from the API (name, git_provider, etc.). Computed attributes cannot be set in .tf
// but must be declared so the provider can store them in state and expose them in show/outputs.
func (r *repositoryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
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
		},
	}
}

func (r *repositoryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", req.ProviderData),
		)
		return
	}
	r.client = c
}

// Create is called on first apply when the resource is in config but not yet in state.
// It activates/deactivates and optionally configures an existing Aikido repo.
func (r *repositoryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan repositoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, err := r.setRepoConfig(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Error configuring repository", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Read is called during refresh/plan to sync the repository from the API into state.
func (r *repositoryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state repositoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.read(ctx, state.ID.ValueString())
	if err != nil {
		if client.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading repository", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, updated)...)
}

// Update is called on apply when config changes in-place (no replacement).
// It applies the new config to the existing Aikido repo.
func (r *repositoryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan repositoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, err := r.setRepoConfig(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Error configuring repository", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete is called when the resource is removed from config or on destroy.
// Deactivates the repo.
func (r *repositoryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state repositoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.setActive(ctx, state.ID.ValueString(), false); err != nil && !client.NotFound(err) {
		resp.Diagnostics.AddError("Error deactivating repository", err.Error())
	}
}

// ImportState lets users adopt an existing Aikido repo into Terraform state by ID.
func (r *repositoryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// configure is shared by Create and Update because both do the same thing for this resource:
// repos already exist in Aikido, so apply only sets active and optional config fields.
// It pushes the planned values to the API, then re-reads the repo for the response state.
func (r *repositoryResource) setRepoConfig(ctx context.Context, plan repositoryModel) (repositoryModel, error) {
	id := plan.ID.ValueString()

	if err := r.setActive(ctx, id, plan.Active.ValueBool()); err != nil {
		return repositoryModel{}, err
	}
	if !plan.Sensitivity.IsNull() && !plan.Sensitivity.IsUnknown() {
		body := map[string]string{"sensitivity": plan.Sensitivity.ValueString()}
		if err := r.client.Do(ctx, "PUT", basePath+"/"+id+"/sensitivity", body, nil); err != nil {
			return repositoryModel{}, fmt.Errorf("updating sensitivity: %w", err)
		}
	}
	if !plan.Connectivity.IsNull() && !plan.Connectivity.IsUnknown() {
		body := map[string]string{"connectivity": plan.Connectivity.ValueString()}
		if err := r.client.Do(ctx, "PUT", basePath+"/"+id+"/connectivity", body, nil); err != nil {
			return repositoryModel{}, fmt.Errorf("updating connectivity: %w", err)
		}
	}

	return r.read(ctx, id)
}

// setActive activates or deactivates the repository.
func (r *repositoryResource) setActive(ctx context.Context, id string, active bool) error {
	codeRepoID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid repository id %q: %w", id, err)
	}

	path := basePath + "/deactivate"
	if active {
		path = basePath + "/activate"
	}
	return r.client.Do(ctx, "POST", path, map[string]int64{"code_repo_id": codeRepoID}, nil)
}

// read reads the repository from the API into the state.
func (r *repositoryResource) read(ctx context.Context, id string) (repositoryModel, error) {
	var repo repositoryAPI
	if err := r.client.Do(ctx, "GET", basePath+"/"+id, nil, &repo); err != nil {
		return repositoryModel{}, err
	}

	state := repositoryModel{
		ID:             types.StringValue(strconv.FormatInt(repo.ID, 10)),
		Active:         types.BoolValue(repo.Active),
		Name:           types.StringValue(repo.Name),
		GitProvider:    types.StringValue(repo.Provider),
		Branch:         types.StringValue(repo.Branch),
		URL:            types.StringValue(repo.URL),
		ExternalRepoID: types.StringValue(repo.ExternalRepoID),
	}
	if repo.Sensitivity != "" {
		state.Sensitivity = types.StringValue(repo.Sensitivity)
	} else {
		state.Sensitivity = types.StringNull()
	}
	if repo.Connectivity != "" {
		state.Connectivity = types.StringValue(repo.Connectivity)
	} else {
		state.Connectivity = types.StringNull()
	}
	return state, nil
}
