package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	prChecksSettingsPath     = "/public/v1/repositories/code/continuous_integration/checks"
	prChecksSettingsPageSize = 100
	prChecksSettingsCacheKey = "repositories/code/continuous_integration/checks"
)

var (
	_ resource.Resource                   = &prChecksSettingsResource{}
	_ resource.ResourceWithImportState    = &prChecksSettingsResource{}
	_ resource.ResourceWithConfigure      = &prChecksSettingsResource{}
	_ resource.ResourceWithValidateConfig = &prChecksSettingsResource{}
)

func NewRepoPRChecksSettingsResource() resource.Resource {
	return &prChecksSettingsResource{}
}

type prChecksSettingsResource struct {
	client *client.Client
}

type prChecksSettingsModel struct {
	ID                                       types.String `tfsdk:"id"`
	CodeRepoID                               types.Int64  `tfsdk:"code_repo_id"`
	MinimumSeverity                          types.String `tfsdk:"minimum_severity"`
	FailOnDependencyScan                     types.Bool   `tfsdk:"fail_on_dependency_scan"`
	FailOnSastScan                           types.Bool   `tfsdk:"fail_on_sast_scan"`
	FailOnIacScan                            types.Bool   `tfsdk:"fail_on_iac_scan"`
	FailOnSecretsScan                        types.Bool   `tfsdk:"fail_on_secrets_scan"`
	FailOnMalwareScan                        types.Bool   `tfsdk:"fail_on_malware_scan"`
	PostInlineCommentsMinSeverity            types.String `tfsdk:"post_inline_comments_min_severity"`
	MinimumLicenseSeverity                   types.String `tfsdk:"minimum_license_severity"`
	FailOnCodeQualityScan                    types.Bool   `tfsdk:"fail_on_code_quality_scan"`
	EnableCodeQualityScan                    types.Bool   `tfsdk:"enable_code_quality_scan"`
	PostCodeQualityInlineCommentsMinSeverity types.String `tfsdk:"post_code_quality_inline_comments_min_severity"`
	RunDeepAuditPRScan                       types.Bool   `tfsdk:"run_deep_audit_pr_scan"`
}

type prChecksSettingsAPI struct {
	ID                                       int64   `json:"id"`
	CodeRepoID                               int64   `json:"code_repo_id"`
	MinimumSeverity                          string  `json:"minimum_severity"`
	FailOnDependencyScan                     bool    `json:"fail_on_dependency_scan"`
	FailOnSastScan                           bool    `json:"fail_on_sast_scan"`
	FailOnIacScan                            bool    `json:"fail_on_iac_scan"`
	FailOnSecretsScan                        bool    `json:"fail_on_secrets_scan"`
	FailOnMalwareScan                        bool    `json:"fail_on_malware_scan"`
	PostInlineCommentsMinSeverity            string  `json:"post_inline_comments_min_severity"`
	MinimumLicenseSeverity                   string  `json:"minimum_license_severity"`
	FailOnCodeQualityScan                    bool    `json:"fail_on_code_quality_scan"`
	EnableCodeQualityScan                    bool    `json:"enable_code_quality_scan"`
	PostCodeQualityInlineCommentsMinSeverity *string `json:"post_code_quality_inline_comments_min_severity"`
	RunDeepAuditPRScan                       bool    `json:"run_deep_audit_pr_scan"`
}

func (r *prChecksSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_repo_pr_checks_settings"
}

func (r *prChecksSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages pull request checks settings for one Aikido code repository. " +
			"The Aikido API has no delete endpoint for PR checks settings, so destroying this resource " +
			"only removes it from Terraform state and leaves the remote settings unchanged.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido PR checks settings ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code_repo_id": schema.Int64Attribute{
				Required:    true,
				Description: "Aikido code repository ID.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"minimum_severity": schema.StringAttribute{
				Required:    true,
				Description: "Minimum severity of new issues for when the CI check fails. One of: low, medium, high, critical, always_pass_check.",
				Validators: []validator.String{
					stringvalidator.OneOf("low", "medium", "high", "critical", "always_pass_check"),
				},
			},
			"fail_on_dependency_scan": schema.BoolAttribute{
				Required:    true,
				Description: "Whether CI checks fail for new dependency issues.",
			},
			"fail_on_sast_scan": schema.BoolAttribute{
				Required:    true,
				Description: "Whether CI checks fail for new SAST issues.",
			},
			"fail_on_iac_scan": schema.BoolAttribute{
				Required:    true,
				Description: "Whether CI checks fail for new IaC issues.",
			},
			"fail_on_secrets_scan": schema.BoolAttribute{
				Required:    true,
				Description: "Whether CI checks fail for new secrets issues.",
			},
			"fail_on_malware_scan": schema.BoolAttribute{
				Required:    true,
				Description: "Whether CI checks fail for new malware issues.",
			},
			"post_inline_comments_min_severity": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Minimum severity for inline comments. Defaults to none when omitted. One of: none, low, medium, high, critical.",
				Validators: []validator.String{
					stringvalidator.OneOf("none", "low", "medium", "high", "critical"),
				},
			},
			"minimum_license_severity": schema.StringAttribute{
				Required: true,
				Description: "Minimum license severity for failing CI checks. One of: none, high, critical. " +
					"Set to none to disable license scanning.",
				Validators: []validator.String{
					stringvalidator.OneOf("none", "high", "critical"),
				},
			},
			"fail_on_code_quality_scan": schema.BoolAttribute{
				Required: true,
				Description: "Whether CI checks fail for new code quality issues. " +
					"Must be false when enable_code_quality_scan is false.",
			},
			"enable_code_quality_scan": schema.BoolAttribute{
				Required:    true,
				Description: "Whether code quality scanning is enabled.",
			},
			"post_code_quality_inline_comments_min_severity": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Minimum severity for code quality inline comments. " +
					"Required when enable_code_quality_scan is true (one of: low, medium, high, critical). " +
					"Ignored when enable_code_quality_scan is false and may be omitted.",
				Validators: []validator.String{
					stringvalidator.OneOf("low", "medium", "high", "critical"),
				},
			},
			"run_deep_audit_pr_scan": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Whether Deep Review is run on pull requests. " +
					"Requires at least one vulnerability scan type to be enabled " +
					"(fail_on_dependency_scan, fail_on_sast_scan, fail_on_iac_scan, fail_on_secrets_scan, fail_on_malware_scan, " +
					"or minimum_license_severity other than none). " +
					"Deep Review is currently only available in the EU region.",
			},
		},
	}
}

func (r *prChecksSettingsResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var settings prChecksSettingsModel
	response.Diagnostics.Append(request.Config.Get(ctx, &settings)...)

	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(validatePRChecksSettingsCodeQuality(settings)...)
	response.Diagnostics.Append(validatePRChecksSettingsDeepAudit(settings)...)
}

func validatePRChecksSettingsCodeQuality(settings prChecksSettingsModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if settings.EnableCodeQualityScan.IsNull() || settings.EnableCodeQualityScan.IsUnknown() {
		return diags
	}

	if settings.EnableCodeQualityScan.ValueBool() {
		if settings.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
			diags.AddAttributeError(
				path.Root("post_code_quality_inline_comments_min_severity"),
				"Missing post_code_quality_inline_comments_min_severity",
				`"post_code_quality_inline_comments_min_severity" is required when "enable_code_quality_scan" is true. Use one of: low, medium, high, critical.`,
			)
		}

		return diags
	}

	if !settings.FailOnCodeQualityScan.IsNull() && !settings.FailOnCodeQualityScan.IsUnknown() && settings.FailOnCodeQualityScan.ValueBool() {
		diags.AddAttributeError(
			path.Root("fail_on_code_quality_scan"),
			"Invalid fail_on_code_quality_scan",
			`"fail_on_code_quality_scan" must be false when "enable_code_quality_scan" is false.`,
		)
	}

	return diags
}

func validatePRChecksSettingsDeepAudit(settings prChecksSettingsModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if settings.RunDeepAuditPRScan.IsNull() || settings.RunDeepAuditPRScan.IsUnknown() || !settings.RunDeepAuditPRScan.ValueBool() {
		return diags
	}

	scanFlags := []types.Bool{
		settings.FailOnDependencyScan,
		settings.FailOnSastScan,
		settings.FailOnIacScan,
		settings.FailOnSecretsScan,
		settings.FailOnMalwareScan,
	}

	enabled := false
	for _, flag := range scanFlags {
		if flag.IsUnknown() {
			return diags
		}

		if !flag.IsNull() && flag.ValueBool() {
			enabled = true
			break
		}
	}

	if !settings.MinimumLicenseSeverity.IsUnknown() && !settings.MinimumLicenseSeverity.IsNull() && settings.MinimumLicenseSeverity.ValueString() != "none" {
		enabled = true
	}

	if !enabled {
		diags.AddAttributeError(
			path.Root("run_deep_audit_pr_scan"),
			"Invalid run_deep_audit_pr_scan",
			`You must enable at least one vulnerability scan type to run Deep Review on pull requests. `+
				`Set one of fail_on_dependency_scan, fail_on_sast_scan, fail_on_iac_scan, fail_on_secrets_scan, `+
				`fail_on_malware_scan to true, or set minimum_license_severity to high or critical.`,
		)
	}

	return diags
}

func (r *prChecksSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *prChecksSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned prChecksSettingsModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setPRChecksSettings(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring PR checks settings for repository "+strconv.FormatInt(planned.CodeRepoID.ValueInt64(), 10), err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *prChecksSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var prior prChecksSettingsModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	apiSettings, err := prChecksSettingsFromCacheForRepo(ctx, r.client, prior.CodeRepoID.ValueInt64())
	if err != nil {
		response.Diagnostics.AddError("Error reading PR checks settings for repository "+strconv.FormatInt(prior.CodeRepoID.ValueInt64(), 10), err.Error())
		return
	}

	if apiSettings == nil {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, mergePRChecksSettingsAPIAndPrior(*apiSettings, &prior))...)
}

func (r *prChecksSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned prChecksSettingsModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setPRChecksSettings(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring PR checks settings for repository "+strconv.FormatInt(planned.CodeRepoID.ValueInt64(), 10), err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

// Delete is a no-op: the Aikido API has no delete endpoint for PR checks settings.
// Destroy only removes the resource from Terraform state; remote settings are left unchanged.
func (r *prChecksSettingsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *prChecksSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	codeRepoID, err := strconv.ParseInt(request.ID, 10, 64)
	if err != nil {
		response.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected numeric code_repo_id, got %q: %v", request.ID, err),
		)

		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("code_repo_id"), codeRepoID)...)
}

func (r *prChecksSettingsResource) setPRChecksSettings(ctx context.Context, planned prChecksSettingsModel) (prChecksSettingsModel, error) {
	if err := r.client.Do(ctx, "POST", prChecksSettingsPath, constructPRChecksSettingsBody(planned), nil); err != nil {
		return prChecksSettingsModel{}, err
	}

	apiSettings, err := prChecksSettingsFromAPI(ctx, r.client, planned.CodeRepoID.ValueInt64())
	if err != nil {
		return prChecksSettingsModel{}, err
	}

	if apiSettings == nil {
		return prChecksSettingsModel{}, fmt.Errorf("settings for repository %d were not found after update", planned.CodeRepoID.ValueInt64())
	}

	return mergePRChecksSettingsAPIAndPrior(*apiSettings, &planned), nil
}

func constructPRChecksSettingsBody(planned prChecksSettingsModel) map[string]any {
	body := map[string]any{
		"code_repo_id":              planned.CodeRepoID.ValueInt64(),
		"minimum_severity":          planned.MinimumSeverity.ValueString(),
		"fail_on_dependency_scan":   planned.FailOnDependencyScan.ValueBool(),
		"fail_on_sast_scan":         planned.FailOnSastScan.ValueBool(),
		"fail_on_iac_scan":          planned.FailOnIacScan.ValueBool(),
		"fail_on_secrets_scan":      planned.FailOnSecretsScan.ValueBool(),
		"fail_on_malware_scan":      planned.FailOnMalwareScan.ValueBool(),
		"minimum_license_severity":  planned.MinimumLicenseSeverity.ValueString(),
		"fail_on_code_quality_scan": planned.FailOnCodeQualityScan.ValueBool(),
		"enable_code_quality_scan":  planned.EnableCodeQualityScan.ValueBool(),
	}

	if planned.EnableCodeQualityScan.ValueBool() {
		body["post_code_quality_inline_comments_min_severity"] = planned.PostCodeQualityInlineCommentsMinSeverity.ValueString()
	}

	if !planned.RunDeepAuditPRScan.IsNull() && !planned.RunDeepAuditPRScan.IsUnknown() {
		body["run_deep_audit_pr_scan"] = planned.RunDeepAuditPRScan.ValueBool()
	}

	if !planned.PostInlineCommentsMinSeverity.IsNull() && !planned.PostInlineCommentsMinSeverity.IsUnknown() {
		body["post_inline_comments_min_severity"] = planned.PostInlineCommentsMinSeverity.ValueString()
	}

	return body
}

// prChecksSettingsList returns the shared paginated list of PR checks settings,
// fetched at most once per Client. A workspace of N repos costs N/page_size
// list GETs instead of N filtered GETs when many resources share one plan.
func prChecksSettingsListFromCache(ctx context.Context, c *client.Client) (map[int64]prChecksSettingsAPI, error) {
	return client.LoadCached(c, ctx, prChecksSettingsCacheKey, func(ctx context.Context) (map[int64]prChecksSettingsAPI, error) {
		items, err := client.FetchAllPages[prChecksSettingsAPI](ctx, c, prChecksSettingsPath, prChecksSettingsPageSize, "")
		if err != nil {
			return nil, err
		}

		settingsCacheMap := make(map[int64]prChecksSettingsAPI, len(items))
		for _, settings := range items {
			settingsCacheMap[settings.CodeRepoID] = settings
		}

		return settingsCacheMap, nil
	})
}

// prChecksSettingsFromCache looks up one repo in the shared list. Use for Read
// when many resources share one plan (avoids N filtered GETs).
func prChecksSettingsFromCacheForRepo(ctx context.Context, c *client.Client, codeRepoID int64) (*prChecksSettingsAPI, error) {
	settings, err := prChecksSettingsListFromCache(ctx, c)
	if err != nil {
		return nil, err
	}

	// lookup the settings in the cache
	cachedSettings, ok := settings[codeRepoID]
	if !ok {
		return nil, nil
	}

	return &cachedSettings, nil
}

// prChecksSettingsFromAPI loads one repo's settings via filter_code_repo_id.
// Use after writes: POST only returns {success:1}, and the list cache may be stale.
func prChecksSettingsFromAPI(ctx context.Context, c *client.Client, codeRepoID int64) (*prChecksSettingsAPI, error) {
	path := prChecksSettingsPath + "?filter_code_repo_id=" + strconv.FormatInt(codeRepoID, 10)

	var settings []prChecksSettingsAPI
	if err := c.Do(ctx, "GET", path, nil, &settings); err != nil {
		return nil, err
	}
	if len(settings) == 0 {
		return nil, nil
	}
	return &settings[0], nil
}

func mapPRChecksSettingsAPIToModel(api prChecksSettingsAPI) prChecksSettingsModel {
	state := prChecksSettingsModel{
		ID:                                       types.StringValue(strconv.FormatInt(api.ID, 10)),
		CodeRepoID:                               types.Int64Value(api.CodeRepoID),
		MinimumSeverity:                          types.StringValue(api.MinimumSeverity),
		FailOnDependencyScan:                     types.BoolValue(api.FailOnDependencyScan),
		FailOnSastScan:                           types.BoolValue(api.FailOnSastScan),
		FailOnIacScan:                            types.BoolValue(api.FailOnIacScan),
		FailOnSecretsScan:                        types.BoolValue(api.FailOnSecretsScan),
		FailOnMalwareScan:                        types.BoolValue(api.FailOnMalwareScan),
		FailOnCodeQualityScan:                    types.BoolValue(api.FailOnCodeQualityScan),
		EnableCodeQualityScan:                    types.BoolValue(api.EnableCodeQualityScan),
		RunDeepAuditPRScan:                       types.BoolValue(api.RunDeepAuditPRScan),
		MinimumLicenseSeverity:                   types.StringValue(api.MinimumLicenseSeverity),
		PostInlineCommentsMinSeverity:            types.StringValue(api.PostInlineCommentsMinSeverity),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
	}

	if api.EnableCodeQualityScan && api.PostCodeQualityInlineCommentsMinSeverity != nil {
		state.PostCodeQualityInlineCommentsMinSeverity = types.StringValue(*api.PostCodeQualityInlineCommentsMinSeverity)
	}

	if api.MinimumLicenseSeverity == "" {
		state.MinimumLicenseSeverity = types.StringValue("none")
	}

	if api.PostInlineCommentsMinSeverity == "" {
		state.PostInlineCommentsMinSeverity = types.StringValue("none")
	}

	return state
}

// mergePRChecksAPIAndPrior returns state from the API, but mirrors prior values for
// fields the API ignores when the related feature is disabled.
func mergePRChecksSettingsAPIAndPrior(api prChecksSettingsAPI, prior *prChecksSettingsModel) prChecksSettingsModel {
	state := mapPRChecksSettingsAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.EnableCodeQualityScan {
		state.PostCodeQualityInlineCommentsMinSeverity = prior.PostCodeQualityInlineCommentsMinSeverity
	}

	return state
}
