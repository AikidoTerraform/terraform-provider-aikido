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
	sastSettingsPath              = "/public/v1/repositories/autofix/sast/settings"
	autofixSastSettingsResourceID = "autofix_sast_settings"
)

var (
	_ resource.Resource                = &autofixSastSettingsResource{}
	_ resource.ResourceWithImportState = &autofixSastSettingsResource{}
	_ resource.ResourceWithConfigure   = &autofixSastSettingsResource{}
)

func NewAutofixSastSettingsResource() resource.Resource {
	return &autofixSastSettingsResource{}
}

type autofixSastSettingsResource struct {
	client *client.Client
}

type sastModel struct {
	ID             types.String `tfsdk:"id"`
	Enabled        types.Bool   `tfsdk:"enabled"`
	SeverityFilter types.String `tfsdk:"severity_filter"`
	ReposScope     types.String `tfsdk:"repos_scope"`
	RepoIDs        []int64      `tfsdk:"repo_ids"`
}

type sastSettingsAPI struct {
	Enabled        bool    `json:"enabled"`
	SeverityFilter string  `json:"severity_filter"`
	ReposScope     string  `json:"repos_scope"`
	RepoIDs        []int64 `json:"repo_ids"`
}

func (r *autofixSastSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_autofix_sast_settings"
}

func (r *autofixSastSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
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
			"severity_filter": schema.StringAttribute{
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

func (r *autofixSastSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *autofixSastSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
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

func (r *autofixSastSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
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

func (r *autofixSastSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
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

func (r *autofixSastSettingsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
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

func (r *autofixSastSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *autofixSastSettingsResource) applySettings(ctx context.Context, planned sastModel) (sastModel, diag.Diagnostics) {
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

func (r *autofixSastSettingsResource) readSettings(ctx context.Context, prior *sastModel) (*sastModel, diag.Diagnostics) {
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
	state.ID = types.StringValue(autofixSastSettingsResourceID)
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
		"enabled":         true,
		"severity_filter": planned.SeverityFilter.ValueString(),
		"repos_scope":     planned.ReposScope.ValueString(),
		"repo_ids":        normalizeIDs(planned.RepoIDs),
	}
}

func mapSastAPIToModel(api sastSettingsAPI) *sastModel {
	return &sastModel{
		Enabled:        types.BoolValue(api.Enabled),
		SeverityFilter: types.StringValue(api.SeverityFilter),
		ReposScope:     types.StringValue(api.ReposScope),
		RepoIDs:        normalizeIDs(api.RepoIDs),
	}
}

func applySastPlanOverrides(state *sastModel, planned *sastModel) {
	if planned == nil || state == nil {
		return
	}

	if !planned.SeverityFilter.IsUnknown() {
		state.SeverityFilter = planned.SeverityFilter
	}

	if !planned.ReposScope.IsUnknown() {
		state.ReposScope = planned.ReposScope
	}

	state.RepoIDs = normalizeIDs(planned.RepoIDs)
}

func mergeSastAPIAndPrior(api sastSettingsAPI, prior *sastModel) *sastModel {
	state := mapSastAPIToModel(api)

	if !api.Enabled {
		state.SeverityFilter = types.StringNull()
		state.ReposScope = types.StringNull()
		state.RepoIDs = []int64{}

		if prior != nil {
			if !prior.SeverityFilter.IsNull() && !prior.SeverityFilter.IsUnknown() {
				state.SeverityFilter = prior.SeverityFilter
			}

			if !prior.ReposScope.IsNull() && !prior.ReposScope.IsUnknown() {
				state.ReposScope = prior.ReposScope
			}

			state.RepoIDs = normalizeIDs(prior.RepoIDs)
		}

		return state
	}

	if api.ReposScope == "all" && prior != nil {
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
