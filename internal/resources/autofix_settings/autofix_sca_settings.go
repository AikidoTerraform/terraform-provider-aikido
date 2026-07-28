package autofix_settings

import (
	"context"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const scaSettingsPath = "/public/v1/repositories/autofix/dependency/settings"

type dependencyModel struct {
	Enabled                  types.Bool   `tfsdk:"enabled"`
	UpgradeType              types.String `tfsdk:"upgrade_type"`
	ReposScope               types.String `tfsdk:"repos_scope"`
	RepoIDs                  []int64      `tfsdk:"repo_ids"`
	UseAikidoLibraryForMajor types.Bool   `tfsdk:"use_aikido_library_for_major"`
}

type scaSettingsAPI struct {
	Enabled                  bool    `json:"enabled"`
	UpgradeType              string  `json:"upgrade_type"`
	ReposScope               string  `json:"repos_scope"`
	RepoIDs                  []int64 `json:"repo_ids"`
	UseAikidoLibraryForMajor bool    `json:"use_aikido_library_for_major"`
}

func dependencySchemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Required:    true,
		Description: "Dependency (SCA / libraries) Autofix settings.",
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				Required:    true,
				Description: "Whether automatic dependency AutoFix PR creation is enabled.",
			},
			"upgrade_type": schema.StringAttribute{
				Required: true,
				Description: "Dependency (libraries) upgrade types to autofix. " +
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
					"Ignored when enabled is false. \"all\" is paying accounts only.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"repo_ids": schema.SetAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for dependency (libraries) autofix when repos_scope is selected. " +
					"Ignored when enabled is false or when repos_scope is all (use []).",
			},
			"use_aikido_library_for_major": schema.BoolAttribute{
				Required:    true,
				Description: "Use Aikido Libraries to avoid major upgrades when available. Ignored when enabled is false.",
			},
		},
	}
}

func getScaSettings(ctx context.Context, apiClient *client.Client) (scaSettingsAPI, error) {
	var response struct {
		Settings scaSettingsAPI `json:"settings"`
	}
	if err := apiClient.Do(ctx, "GET", scaSettingsPath, nil, &response); err != nil {
		return scaSettingsAPI{}, err
	}
	return response.Settings, nil
}

func upsertScaSettings(ctx context.Context, apiClient *client.Client, planned *dependencyModel) error {
	return apiClient.Do(ctx, "PUT", scaSettingsPath, constructScaBody(planned), nil)
}

func constructScaBody(planned *dependencyModel) map[string]any {
	if !planned.Enabled.ValueBool() {
		return map[string]any{"enabled": false}
	}

	return map[string]any{
		"enabled":                      true,
		"upgrade_type":                 planned.UpgradeType.ValueString(),
		"dependency_repos_scope":       planned.ReposScope.ValueString(),
		"dependency_repo_ids":          normalizeIDs(planned.RepoIDs),
		"use_aikido_library_for_major": planned.UseAikidoLibraryForMajor.ValueBool(),
	}
}

func mapScaAPIToModel(api scaSettingsAPI) *dependencyModel {
	return &dependencyModel{
		Enabled:                  types.BoolValue(api.Enabled),
		UpgradeType:              types.StringValue(api.UpgradeType),
		ReposScope:               types.StringValue(api.ReposScope),
		RepoIDs:                  normalizeIDs(api.RepoIDs),
		UseAikidoLibraryForMajor: types.BoolValue(api.UseAikidoLibraryForMajor),
	}
}

// Prefer planned values for fields the API may rewrite when dependency autofix is
// disabled or when repos_scope is all.
func applyScaPlanOverrides(state *dependencyModel, planned *dependencyModel) {
	if planned == nil || state == nil {
		return
	}

	if !planned.UpgradeType.IsUnknown() {
		state.UpgradeType = planned.UpgradeType
	}

	if !planned.ReposScope.IsUnknown() {
		state.ReposScope = planned.ReposScope
	}

	state.RepoIDs = normalizeIDs(planned.RepoIDs)
	if !planned.UseAikidoLibraryForMajor.IsUnknown() {
		state.UseAikidoLibraryForMajor = planned.UseAikidoLibraryForMajor
	}
}

func mergeScaAPIAndPrior(api scaSettingsAPI, prior *dependencyModel) *dependencyModel {
	state := mapScaAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.Enabled {
		if !prior.UpgradeType.IsNull() && !prior.UpgradeType.IsUnknown() {
			state.UpgradeType = prior.UpgradeType
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
