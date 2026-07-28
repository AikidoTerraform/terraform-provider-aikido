package autofix_settings

import (
	"context"
	"fmt"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const basePath = "/public/v1/repositories/autofix/settings"

var (
	_ resource.Resource              = &autofixSettingsResource{}
	_ resource.ResourceWithConfigure = &autofixSettingsResource{}
)

func NewResource() resource.Resource {
	return &autofixSettingsResource{}
}

type autofixSettingsResource struct {
	client *client.Client
}

type autofixSettingsModel struct {
	Enabled                  types.Bool   `tfsdk:"enabled"`
	UpgradeType              types.String `tfsdk:"upgrade_type"`
	DependencyReposScope     types.String `tfsdk:"dependency_repos_scope"`
	DependencyRepoIDs        []int64      `tfsdk:"dependency_repo_ids"`
	UseAikidoLibraryForMajor types.Bool   `tfsdk:"use_aikido_library_for_major"`
	PentestAutofixType       types.String `tfsdk:"pentest_autofix_type"`
	SastAutofixType          types.String `tfsdk:"sast_autofix_type"`
	SastReposScope           types.String `tfsdk:"sast_repos_scope"`
	SastRepoIDs              []int64      `tfsdk:"sast_repo_ids"`
}

type autofixSettingsAPI struct {
	Enabled                  bool    `json:"enabled"`
	UpgradeType              string  `json:"upgrade_type"`
	DependencyReposScope     string  `json:"dependency_repos_scope"`
	DependencyRepoIDs        []int64 `json:"dependency_repo_ids"`
	UseAikidoLibraryForMajor bool    `json:"use_aikido_library_for_major"`
	PentestAutofixType       string  `json:"pentest_autofix_type"`
	SastAutofixType          string  `json:"sast_autofix_type"`
	SastReposScope           string  `json:"sast_repos_scope"`
	SastRepoIDs              []int64 `json:"sast_repo_ids"`
}

func (r *autofixSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_autofix_settings"
}

func (r *autofixSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages workspace Autofix settings for automatic AutoFix PR creation. " +
			"When enabled is false, the API forces upgrade_type to none and dependency_repo_ids to an empty set and disables automatic depdendency autofix PR creation. " +
			"When sast_autofix_type is none, the API may force sast_repos_scope to none and clear sast_repo_ids and disables automatic SAST autofix PR creation. " +
			"Repo ID sets are ignored when the corresponding scope is all.",
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				Required:    true,
				Description: "Whether automatic dependency AutoFix PR creation is enabled.",
			},
			"upgrade_type": schema.StringAttribute{
				Required: true,
				Description: "Dependency (libraries) upgrade types to autofix. Use none to disable dependency autofix. " +
					"Ignored when enabled is false (API forces none). " +
					"One of: upgrade_all_packages, minor_and_patch_versions_only, critical_issues_only, critical_and_high_only, none.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						"upgrade_all_packages",
						"minor_and_patch_versions_only",
						"critical_issues_only",
						"critical_and_high_only",
						"none",
					),
				},
			},
			"dependency_repos_scope": schema.StringAttribute{
				Required: true,
				Description: "Scope of the dependency (libraries) autofix. One of: all, selected. " +
					"Ignored when enabled is false.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"dependency_repo_ids": schema.SetAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for dependency (libraries) autofix. " +
					"Ignored when enabled is false (API forces an empty set).",
			},
			"use_aikido_library_for_major": schema.BoolAttribute{
				Required:    true,
				Description: "Use Aikido Libraries to avoid major upgrades when available.",
			},
			"pentest_autofix_type": schema.StringAttribute{
				Required: true,
				Description: "Severity filter for Pentest & AI Code Analysis autofix. Use none to disable automatic pentest autofix PR creation. " +
					"One of: all, critical_and_high_only, none.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "critical_and_high_only", "none"),
				},
			},
			"sast_autofix_type": schema.StringAttribute{
				Required: true,
				Description: "Severity filter for SAST & IaC autofix. Use none to disable automatic SAST autofix PR creation. " +
					"One of: critical_issues_only, critical_and_high_only, all, none.",
				Validators: []validator.String{
					stringvalidator.OneOf("critical_issues_only", "critical_and_high_only", "all", "none"),
				},
			},
			"sast_repos_scope": schema.StringAttribute{
				Required: true,
				Description: "Scope of the SAST & IaC autofix. One of: all, selected. " +
					"Ignored when sast_autofix_type is none.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"sast_repo_ids": schema.SetAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for SAST & IaC autofix. " +
					"Ignored when sast_autofix_type is none.",
			},
		},
	}
}

func (r *autofixSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *autofixSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned autofixSettingsModel

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

func (r *autofixSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState autofixSettingsModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	apiSettings, err := r.getAutofixSettings(ctx)
	if err != nil {
		if client.NotFound(err) {
			response.State.RemoveResource(ctx)
			return
		}

		response.Diagnostics.AddError("Error reading autofix settings", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, mergeAPIAndPriorState(apiSettings, priorState))...)
}

func (r *autofixSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned autofixSettingsModel

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

func (r *autofixSettingsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState autofixSettingsModel

	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	// Destroy disables automatic PR creation. Other required fields stay known.
	body := map[string]any{
		"enabled":                      false,
		"use_aikido_library_for_major": priorState.UseAikidoLibraryForMajor.ValueBool(),
		"pentest_autofix_type":         priorState.PentestAutofixType.ValueString(),
		"sast_autofix_type":            priorState.SastAutofixType.ValueString(),
		"sast_repos_scope":             priorState.SastReposScope.ValueString(),
		"sast_repo_ids":                normalizeIDs(priorState.SastRepoIDs),
		"dependency_repo_ids":          normalizeIDs(priorState.DependencyRepoIDs),
		"dependency_repos_scope":       priorState.DependencyReposScope.ValueString(),
		"upgrade_type":                 priorState.UpgradeType.ValueString(),
	}

	if err := r.client.Do(ctx, "PUT", basePath, body, nil); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error disabling autofix settings", err.Error())
	}
}

// applySettings upserts the planned config and updates state.
func (r *autofixSettingsResource) applySettings(ctx context.Context, planned autofixSettingsModel) (autofixSettingsModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if err := r.client.Do(ctx, "PUT", basePath, constructBody(planned), nil); err != nil {
		diags.AddError("Error configuring autofix settings", err.Error())
		return autofixSettingsModel{}, diags
	}

	apiSettings, err := r.getAutofixSettings(ctx)
	if err != nil {
		diags.AddError("Error reading autofix settings after update", err.Error())
		return autofixSettingsModel{}, diags
	}

	state := mapApiResponseToStateModel(apiSettings)

	// Prefer the plan over the GET response for fields the API may rewrite
	// when they are unused (e.g. enabled=false → upgrade_type "none" and empty
	// dependency_repo_ids; sast_autofix_type=none → sast_repos_scope "none"
	// and empty sast_repo_ids). Apply state must match the plan for known
	// attributes, or Terraform errors with "provider produced inconsistent result".
	if !planned.UpgradeType.IsUnknown() {
		state.UpgradeType = planned.UpgradeType
	}
	if !planned.DependencyReposScope.IsUnknown() {
		state.DependencyReposScope = planned.DependencyReposScope
	}
	if !planned.SastReposScope.IsUnknown() {
		state.SastReposScope = planned.SastReposScope
	}

	state.DependencyRepoIDs = normalizeIDs(planned.DependencyRepoIDs)
	state.SastRepoIDs = normalizeIDs(planned.SastRepoIDs)

	return state, diags
}

func (r *autofixSettingsResource) getAutofixSettings(ctx context.Context) (autofixSettingsAPI, error) {
	// The GET response nests the settings under a "settings" key.
	var settinsApiResponse struct {
		Settings autofixSettingsAPI `json:"settings"`
	}

	if err := r.client.Do(ctx, "GET", basePath, nil, &settinsApiResponse); err != nil {
		return autofixSettingsAPI{}, err
	}

	return settinsApiResponse.Settings, nil
}

func constructBody(planned autofixSettingsModel) map[string]any {
	body := map[string]any{
		"enabled":                      planned.Enabled.ValueBool(),
		"use_aikido_library_for_major": planned.UseAikidoLibraryForMajor.ValueBool(),
		"pentest_autofix_type":         planned.PentestAutofixType.ValueString(),
		"sast_autofix_type":            planned.SastAutofixType.ValueString(),
		"sast_repos_scope":             planned.SastReposScope.ValueString(),
		"sast_repo_ids":                normalizeIDs(planned.SastRepoIDs),
		"dependency_repo_ids":          normalizeIDs(planned.DependencyRepoIDs),
	}

	if !planned.UpgradeType.IsNull() && !planned.UpgradeType.IsUnknown() {
		body["upgrade_type"] = planned.UpgradeType.ValueString()
	}

	if !planned.DependencyReposScope.IsNull() && !planned.DependencyReposScope.IsUnknown() {
		body["dependency_repos_scope"] = planned.DependencyReposScope.ValueString()
	}

	return body
}

func mapApiResponseToStateModel(api autofixSettingsAPI) autofixSettingsModel {
	return autofixSettingsModel{
		Enabled:                  types.BoolValue(api.Enabled),
		UpgradeType:              types.StringValue(api.UpgradeType),
		DependencyReposScope:     types.StringValue(api.DependencyReposScope),
		DependencyRepoIDs:        normalizeIDs(api.DependencyRepoIDs),
		UseAikidoLibraryForMajor: types.BoolValue(api.UseAikidoLibraryForMajor),
		PentestAutofixType:       types.StringValue(api.PentestAutofixType),
		SastAutofixType:          types.StringValue(api.SastAutofixType),
		SastReposScope:           types.StringValue(api.SastReposScope),
		SastRepoIDs:              normalizeIDs(api.SastRepoIDs),
	}
}

// mergeAPIAndPriorState merges the API response with the prior state.
// This resolves the issue where the API response is not always consistent with the plan.
// Read refresh reintroduces API-rewritten Autofix fields, causing perpetual drift
func mergeAPIAndPriorState(api autofixSettingsAPI, prior autofixSettingsModel) autofixSettingsModel {
	state := mapApiResponseToStateModel(api)

	if !api.Enabled {
		if !prior.UpgradeType.IsNull() && !prior.UpgradeType.IsUnknown() {
			state.UpgradeType = prior.UpgradeType
		}
		if !prior.DependencyReposScope.IsNull() && !prior.DependencyReposScope.IsUnknown() {
			state.DependencyReposScope = prior.DependencyReposScope
		}
		state.DependencyRepoIDs = normalizeIDs(prior.DependencyRepoIDs)
	} else if api.DependencyReposScope == "all" {
		state.DependencyRepoIDs = normalizeIDs(prior.DependencyRepoIDs)
	}

	if api.SastAutofixType == "none" {
		if !prior.SastReposScope.IsNull() && !prior.SastReposScope.IsUnknown() {
			state.SastReposScope = prior.SastReposScope
		}
		state.SastRepoIDs = normalizeIDs(prior.SastRepoIDs)
	} else if api.SastReposScope == "all" {
		state.SastRepoIDs = normalizeIDs(prior.SastRepoIDs)
	}

	return state
}

func normalizeIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}

	return ids
}
