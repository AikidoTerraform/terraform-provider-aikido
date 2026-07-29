package resources

import (
	"context"
	"fmt"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	dependencySettingsPath              = "/public/v1/repositories/autofix/dependency/settings"
	autofixDependencySettingsResourceID = "autofix_dependency_settings"
)

var (
	_ resource.Resource                = &autofixDependencySettingsResource{}
	_ resource.ResourceWithImportState = &autofixDependencySettingsResource{}
	_ resource.ResourceWithConfigure   = &autofixDependencySettingsResource{}
)

func NewAutofixDependencySettingsResource() resource.Resource {
	return &autofixDependencySettingsResource{}
}

type autofixDependencySettingsResource struct {
	client *client.Client
}

type dependencyModel struct {
	ID                       types.String `tfsdk:"id"`
	Enabled                  types.Bool   `tfsdk:"enabled"`
	SeverityFilter           types.String `tfsdk:"severity_filter"`
	ReposScope               types.String `tfsdk:"repos_scope"`
	RepoIDs                  []int64      `tfsdk:"repo_ids"`
	UseAikidoLibraryForMajor types.Bool   `tfsdk:"use_aikido_library_for_major"`
}

type dependencySettingsAPI struct {
	Enabled                  bool    `json:"enabled"`
	SeverityFilter           string  `json:"severity_filter"`
	ReposScope               string  `json:"repos_scope"`
	RepoIDs                  []int64 `json:"repo_ids"`
	UseAikidoLibraryForMajor bool    `json:"use_aikido_library_for_major"`
}

func (r *autofixDependencySettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_autofix_dependency_settings"
}

func (r *autofixDependencySettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages workspace dependency (libraries) Autofix settings for automatic AutoFix PR creation. " +
			"There is exactly one dependency Autofix settings object per workspace. " +
			"When enabled is false, other fields are ignored by the API. " +
			"Repo ID sets are ignored when repos_scope is all. " +
			"Destroying this resource disables automatic dependency AutoFix PR creation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace dependency Autofix settings identifier.",
			},
			"enabled": schema.BoolAttribute{
				Required:    true,
				Description: "Whether automatic dependency AutoFix PR creation is enabled.",
			},
			"severity_filter": schema.StringAttribute{
				Required: true,
				Description: "Dependency (libraries) severity types to autofix. " +
					"Ignored when enabled is false. " +
					"One of: upgrade_all_packages, minor_and_patch_versions_only, critical_issues_only, critical_and_high_only.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						"upgrade_all_packages",
						"minor_and_patch_versions_only",
						"critical_issues_only",
						"critical_and_high_only",
					),
				},
			},
			"repos_scope": schema.StringAttribute{
				Required: true,
				Description: "Scope of the dependency (libraries) autofix. One of: all, selected. " +
					"Ignored when enabled is false.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"repo_ids": schema.SetAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for dependency (libraries) autofix when repos_scope is selected. " +
					"Ignored when enabled is false or when repos_scope is all.",
			},
			"use_aikido_library_for_major": schema.BoolAttribute{
				Required:    true,
				Description: "Use Aikido Libraries to avoid major upgrades when available. Ignored when enabled is false.",
			},
		},
	}
}

func (r *autofixDependencySettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *autofixDependencySettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned dependencyModel

	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.applySettings(ctx, planned)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *autofixDependencySettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState dependencyModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.readSettings(ctx, &priorState)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	if state == nil {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *autofixDependencySettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned dependencyModel

	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.applySettings(ctx, planned)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *autofixDependencySettingsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState dependencyModel

	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	disabled := &dependencyModel{Enabled: types.BoolValue(false)}
	if err := upsertDependencySettings(ctx, r.client, disabled); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error disabling dependency autofix settings", err.Error())
	}
}

func (r *autofixDependencySettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *autofixDependencySettingsResource) applySettings(ctx context.Context, planned dependencyModel) (dependencyModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if err := upsertDependencySettings(ctx, r.client, &planned); err != nil {
		diags.AddError("Error configuring dependency autofix settings", err.Error())
		return dependencyModel{}, diags
	}

	state, readDiags := r.readSettings(ctx, &planned)
	diags.Append(readDiags...)
	if diags.HasError() || state == nil {
		return dependencyModel{}, diags
	}

	applyDependencyPlanOverrides(state, &planned)
	return *state, diags
}

func (r *autofixDependencySettingsResource) readSettings(ctx context.Context, prior *dependencyModel) (*dependencyModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	api, err := getDependencySettings(ctx, r.client)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}

		diags.AddError("Error reading dependency autofix settings", err.Error())
		return nil, diags
	}

	state := mergeDependencyAPIAndPrior(api, prior)
	state.ID = types.StringValue(autofixDependencySettingsResourceID)
	return state, diags
}

func getDependencySettings(ctx context.Context, apiClient *client.Client) (dependencySettingsAPI, error) {
	var response struct {
		Settings dependencySettingsAPI `json:"settings"`
	}
	if err := apiClient.Do(ctx, "GET", dependencySettingsPath, nil, &response); err != nil {
		return dependencySettingsAPI{}, err
	}
	return response.Settings, nil
}

func upsertDependencySettings(ctx context.Context, apiClient *client.Client, planned *dependencyModel) error {
	return apiClient.Do(ctx, "PUT", dependencySettingsPath, constructDependencyBody(planned), nil)
}

func constructDependencyBody(planned *dependencyModel) map[string]any {
	if !planned.Enabled.ValueBool() {
		return map[string]any{"enabled": false}
	}

	return map[string]any{
		"enabled":                      true,
		"severity_filter":              planned.SeverityFilter.ValueString(),
		"repos_scope":                  planned.ReposScope.ValueString(),
		"repo_ids":                     normalizeIDs(planned.RepoIDs),
		"use_aikido_library_for_major": planned.UseAikidoLibraryForMajor.ValueBool(),
	}
}

func mapDependencyAPIToModel(api dependencySettingsAPI) *dependencyModel {
	return &dependencyModel{
		Enabled:                  types.BoolValue(api.Enabled),
		SeverityFilter:           types.StringValue(api.SeverityFilter),
		ReposScope:               types.StringValue(api.ReposScope),
		RepoIDs:                  normalizeIDs(api.RepoIDs),
		UseAikidoLibraryForMajor: types.BoolValue(api.UseAikidoLibraryForMajor),
	}
}

// Prefer planned values for fields the API may rewrite when dependency autofix is
// disabled or when repos_scope is all.
func applyDependencyPlanOverrides(state *dependencyModel, planned *dependencyModel) {
	if planned == nil || state == nil {
		return
	}

	if !planned.SeverityFilter.IsUnknown() {
		state.SeverityFilter = planned.SeverityFilter
	}

	if !planned.ReposScope.IsUnknown() {
		state.ReposScope = planned.ReposScope
	}

	if !state.Enabled.ValueBool() || state.ReposScope.ValueString() == "all" {
		state.RepoIDs = normalizeIDs(planned.RepoIDs)
	}
	if !planned.UseAikidoLibraryForMajor.IsUnknown() {
		state.UseAikidoLibraryForMajor = planned.UseAikidoLibraryForMajor
	}
}

func mergeDependencyAPIAndPrior(api dependencySettingsAPI, prior *dependencyModel) *dependencyModel {
	state := mapDependencyAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.Enabled {
		if !prior.SeverityFilter.IsNull() && !prior.SeverityFilter.IsUnknown() {
			state.SeverityFilter = prior.SeverityFilter
		}

		if !prior.ReposScope.IsNull() && !prior.ReposScope.IsUnknown() {
			state.ReposScope = prior.ReposScope
		}

		state.RepoIDs = normalizeIDs(prior.RepoIDs)
		if !prior.UseAikidoLibraryForMajor.IsNull() && !prior.UseAikidoLibraryForMajor.IsUnknown() {
			state.UseAikidoLibraryForMajor = prior.UseAikidoLibraryForMajor
		}
	} else if api.ReposScope == "all" {
		state.RepoIDs = normalizeIDs(prior.RepoIDs)
	}

	return state
}

func normalizeIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}

	return ids
}
