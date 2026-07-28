package autofix_settings

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
	sastSettingsPath       = "/public/v1/repositories/autofix/sast/settings"
	sastSettingsResourceID = "autofix_sast_settings"
)

var (
	_ resource.Resource                = &sastSettingsResource{}
	_ resource.ResourceWithImportState = &sastSettingsResource{}
	_ resource.ResourceWithConfigure   = &sastSettingsResource{}
)

func NewSastResource() resource.Resource {
	return &sastSettingsResource{}
}

type sastSettingsResource struct {
	client *client.Client
}

type sastModel struct {
	ID          types.String `tfsdk:"id"`
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

func (r *sastSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_autofix_sast_settings"
}

func (r *sastSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages workspace SAST & IaC Autofix settings for automatic AutoFix PR creation. " +
			"There is exactly one SAST Autofix settings object per workspace. " +
			"When enabled is false, other fields are ignored by the API. " +
			"Repo ID sets are ignored when repos_scope is all. " +
			"Destroying this resource disables automatic SAST & IaC AutoFix PR creation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace SAST Autofix settings identifier.",
			},
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
					"Ignored when enabled is false.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"repo_ids": schema.SetAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for SAST & IaC autofix when repos_scope is selected. " +
					"Ignored when enabled is false or when repos_scope is all.",
			},
		},
	}
}

func (r *sastSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *sastSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned sastModel

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

func (r *sastSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState sastModel
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

func (r *sastSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned sastModel

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

func (r *sastSettingsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState sastModel

	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	disabled := &sastModel{Enabled: types.BoolValue(false)}
	if err := upsertSastSettings(ctx, r.client, disabled); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error disabling SAST autofix settings", err.Error())
	}
}

func (r *sastSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *sastSettingsResource) applySettings(ctx context.Context, planned sastModel) (sastModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if err := upsertSastSettings(ctx, r.client, &planned); err != nil {
		diags.AddError("Error configuring SAST autofix settings", err.Error())
		return sastModel{}, diags
	}

	state, readDiags := r.readSettings(ctx, &planned)
	diags.Append(readDiags...)
	if diags.HasError() || state == nil {
		return sastModel{}, diags
	}

	applySastPlanOverrides(state, &planned)
	return *state, diags
}

func (r *sastSettingsResource) readSettings(ctx context.Context, prior *sastModel) (*sastModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	api, err := getSastSettings(ctx, r.client)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}

		diags.AddError("Error reading SAST autofix settings", err.Error())
		return nil, diags
	}

	state := mergeSastAPIAndPrior(api, prior)
	state.ID = types.StringValue(sastSettingsResourceID)
	return state, diags
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

func normalizeIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}

	return ids
}
