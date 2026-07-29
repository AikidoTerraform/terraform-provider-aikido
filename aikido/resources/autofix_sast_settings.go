package resources

import (
	"context"
	"fmt"
	"slices"

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
	_ resource.Resource                   = &autofixSastSettingsResource{}
	_ resource.ResourceWithImportState    = &autofixSastSettingsResource{}
	_ resource.ResourceWithConfigure      = &autofixSastSettingsResource{}
	_ resource.ResourceWithValidateConfig = &autofixSastSettingsResource{}
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
			"When enabled is false, the other fields are ignored by the API and may be omitted. " +
			"When enabled is true, severity_filter, repos_scope, and repo_ids are required. " +
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
				Optional: true,
				Description: "Severity filter for SAST & IaC autofix. " +
					"Required when enabled is true; may be omitted when enabled is false. " +
					"One of: critical_issues_only, critical_and_high_only, all.",
				Validators: []validator.String{
					stringvalidator.OneOf("critical_issues_only", "critical_and_high_only", "all"),
				},
			},
			"repos_scope": schema.StringAttribute{
				Optional: true,
				Description: "Scope of the SAST & IaC autofix. One of: all, selected. " +
					"Required when enabled is true; may be omitted when enabled is false. " +
					"One of: all, selected.",
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"repo_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.Int64Type,
				Description: "Code repository IDs for SAST & IaC autofix. " +
					"Required (non-empty) when repos_scope is selected. " +
					"Ignored when enabled is false or when repos_scope is all. " +
					"The API may drop invalid or inactive IDs, which fails the apply with an actionable error.",
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

// ValidateConfig enforces the "required when enabled" rules at plan time.
func (r *autofixSastSettingsResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var (
		enabled  types.Bool
		severity types.String
		scope    types.String
		repoIDs  types.Set
	)

	response.Diagnostics.Append(request.Config.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	response.Diagnostics.Append(request.Config.GetAttribute(ctx, path.Root("severity_filter"), &severity)...)
	response.Diagnostics.Append(request.Config.GetAttribute(ctx, path.Root("repos_scope"), &scope)...)
	response.Diagnostics.Append(request.Config.GetAttribute(ctx, path.Root("repo_ids"), &repoIDs)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(validateSastConfig(enabled, severity, scope, repoIDs)...)
}

// validateSastConfig is split out so it is unit-testable without a tfsdk.Config.
func validateSastConfig(enabled types.Bool, severity, scope types.String, repoIDs types.Set) diag.Diagnostics {
	var diags diag.Diagnostics

	// Only enabled=true has cross-field requirements; skip when null/unknown.
	if enabled.IsNull() || enabled.IsUnknown() || !enabled.ValueBool() {
		return diags
	}

	if severity.IsNull() {
		diags.AddAttributeError(path.Root("severity_filter"), "Missing severity_filter",
			`"severity_filter" is required when "enabled" is true.`)
	}
	if scope.IsNull() {
		diags.AddAttributeError(path.Root("repos_scope"), "Missing repos_scope",
			`"repos_scope" is required when "enabled" is true.`)
	}

	if !scope.IsNull() && !scope.IsUnknown() && scope.ValueString() == "selected" {
		if repoIDs.IsNull() || (!repoIDs.IsUnknown() && len(repoIDs.Elements()) == 0) {
			diags.AddAttributeError(path.Root("repo_ids"), "Missing repo_ids",
				`"repo_ids" must contain at least one repository ID when "repos_scope" is "selected".`)
		}
	}

	return diags
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
		response.Diagnostics.AddError("Error disabling SAST & IaC autofix settings", err.Error())
	}
}

func (r *autofixSastSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *autofixSastSettingsResource) applySettings(ctx context.Context, planned sastModel) (sastModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if err := upsertSastSettings(ctx, r.client, &planned); err != nil {
		diags.AddError("Error configuring SAST & IaC autofix settings", err.Error())
		return sastModel{}, diags
	}

	state, readDiags := r.readSettings(ctx, &planned)
	diags.Append(readDiags...)
	if diags.HasError() || state == nil {
		return sastModel{}, diags
	}

	if planned.Enabled.ValueBool() && planned.ReposScope.ValueString() == "selected" {
		if dropped := droppedRepoIDs(planned.RepoIDs, state.RepoIDs); len(dropped) > 0 {
			diags.AddError(
				"Error configuring SAST & IaC autofix settings",
				fmt.Sprintf(
					"Aikido ignored invalid or inactive repository IDs: %v. Remove those IDs from repo_ids and apply again.",
					dropped,
				),
			)
			return sastModel{}, diags
		}
	}

	return *state, diags
}

func (r *autofixSastSettingsResource) readSettings(ctx context.Context, prior *sastModel) (*sastModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	api, err := getSastSettings(ctx, r.client)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}

		diags.AddError("Error reading SAST & IaC autofix settings", err.Error())
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

// mergeSastAPIAndPrior returns state from the API, but mirrors prior state
// verbatim for fields the API ignores (all fields when disabled; repo_ids when
// scope is all) so omitted/null values don't produce spurious diffs.
func mergeSastAPIAndPrior(api sastSettingsAPI, prior *sastModel) *sastModel {
	state := mapSastAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.Enabled {
		state.SeverityFilter = prior.SeverityFilter
		state.ReposScope = prior.ReposScope
		state.RepoIDs = prior.RepoIDs
		return state
	}

	if api.ReposScope == "all" {
		state.RepoIDs = prior.RepoIDs
	}

	return state
}

func normalizeIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}

	return ids
}

func droppedRepoIDs(planned []int64, actual []int64) []int64 {
	actualSet := make(map[int64]struct{}, len(actual))
	for _, id := range actual {
		actualSet[id] = struct{}{}
	}

	var dropped []int64
	seen := make(map[int64]struct{}, len(planned))
	for _, id := range planned {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		if _, ok := actualSet[id]; !ok {
			dropped = append(dropped, id)
		}
	}

	slices.Sort(dropped)
	return dropped
}
