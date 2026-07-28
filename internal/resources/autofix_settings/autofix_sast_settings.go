package autofix_settings

import (
	"context"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const sastSettingsPath = "/public/v1/repositories/autofix/sast/settings"

type sastModel struct {
	Enabled     types.Bool   `tfsdk:"enabled"`
	AutofixType types.String `tfsdk:"autofix_type"`
	ReposScope  types.String `tfsdk:"repos_scope"`
	RepoIDs     []int64      `tfsdk:"repo_ids"`
}

type sastSettingsAPI struct {
	Enabled     bool    `json:"enabled"`
	AutofixType string  `json:"autofix_type"`
	ReposScope  string  `json:"repos_scope"`
	RepoIDs     []int64 `json:"repo_ids"`
}

func sastSchemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Required:    true,
		Description: "SAST & IaC Autofix settings.",
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				Required:    true,
				Description: "Whether automatic SAST & IaC AutoFix PR creation is enabled.",
			},
			"autofix_type": schema.StringAttribute{
				Required: true,
				Description: "Severity filter for SAST & IaC autofix. " +
					"Ignored when enabled is false. " +
					"One of: critical_issues_only, critical_and_high_only, all.",
				Validators: []validator.String{
					stringvalidator.OneOf("critical_issues_only", "critical_and_high_only", "all"),
				},
			},
			"repos_scope": schema.StringAttribute{
				Required: true,
				Description: "Scope of the SAST & IaC autofix. One of: all, selected. " +
					"Ignored when enabled is false. \"all\" is paying accounts only.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"repo_ids": schema.SetAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for SAST & IaC autofix when repos_scope is selected. " +
					"Ignored when enabled is false or when repos_scope is all (use []).",
			},
		},
	}
}

func getSastSettings(ctx context.Context, apiClient *client.Client) (sastSettingsAPI, error) {
	var response struct {
		Settings sastSettingsAPI `json:"settings"`
	}

	if err := apiClient.Do(ctx, "GET", sastSettingsPath, nil, &response); err != nil {
		return sastSettingsAPI{}, err
	}

	return response.Settings, nil
}

func upsertSastSettings(ctx context.Context, apiClient *client.Client, planned *sastModel) error {
	return apiClient.Do(ctx, "PUT", sastSettingsPath, constructSastBody(planned), nil)
}

func constructSastBody(planned *sastModel) map[string]any {
	if !planned.Enabled.ValueBool() {
		return map[string]any{"enabled": false}
	}

	return map[string]any{
		"enabled":      true,
		"autofix_type": planned.AutofixType.ValueString(),
		"repos_scope":  planned.ReposScope.ValueString(),
		"repo_ids":     normalizeIDs(planned.RepoIDs),
	}
}

func mapSastAPIToModel(api sastSettingsAPI) *sastModel {
	return &sastModel{
		Enabled:     types.BoolValue(api.Enabled),
		AutofixType: types.StringValue(api.AutofixType),
		ReposScope:  types.StringValue(api.ReposScope),
		RepoIDs:     normalizeIDs(api.RepoIDs),
	}
}

func applySastPlanOverrides(state *sastModel, planned *sastModel) {
	if planned == nil || state == nil {
		return
	}

	if !planned.AutofixType.IsUnknown() {
		state.AutofixType = planned.AutofixType
	}

	if !planned.ReposScope.IsUnknown() {
		state.ReposScope = planned.ReposScope
	}

	state.RepoIDs = normalizeIDs(planned.RepoIDs)
}

func mergeSastAPIAndPrior(api sastSettingsAPI, prior *sastModel) *sastModel {
	state := mapSastAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.Enabled {
		if !prior.AutofixType.IsNull() && !prior.AutofixType.IsUnknown() {
			state.AutofixType = prior.AutofixType
		}

		if !prior.ReposScope.IsNull() && !prior.ReposScope.IsUnknown() {
			state.ReposScope = prior.ReposScope
		}

		state.RepoIDs = normalizeIDs(prior.RepoIDs)
	} else if api.ReposScope == "all" {
		state.RepoIDs = normalizeIDs(prior.RepoIDs)
	}

	return state
}
