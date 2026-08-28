package resources

import (
	"context"
	"fmt"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	defaultPRChecksSettingsPath       = "/public/v1/repositories/code/continuous_integration/checks/default"
	defaultPRChecksSettingsResourceID = "default_pr_checks_settings"
)

var (
	_ resource.Resource                   = &defaultPRChecksSettingsResource{}
	_ resource.ResourceWithImportState    = &defaultPRChecksSettingsResource{}
	_ resource.ResourceWithConfigure      = &defaultPRChecksSettingsResource{}
	_ resource.ResourceWithValidateConfig = &defaultPRChecksSettingsResource{}
)

func NewDefaultPRChecksSettingsResource() resource.Resource {
	return &defaultPRChecksSettingsResource{}
}

type defaultPRChecksSettingsResource struct {
	client *client.Client
}

type defaultPRChecksSettingsModel struct {
	ID                                       types.String `tfsdk:"id"`
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

type defaultPRChecksSettingsAPI struct {
	IsEnabled                                bool    `json:"is_enabled"`
	MinimumSeverity                          string  `json:"minimum_severity"`
	FailOnDependencyScan                     bool    `json:"fail_on_dependency_scan"`
	FailOnSastScan                           bool    `json:"fail_on_sast_scan"`
	FailOnIacScan                            bool    `json:"fail_on_iac_scan"`
	FailOnSecretsScan                        bool    `json:"fail_on_secrets_scan"`
	FailOnMalwareScan                        bool    `json:"fail_on_malware_scan"`
	PostInlineCommentsMinSeverity            *string `json:"post_inline_comments_min_severity"`
	MinimumLicenseSeverity                   string  `json:"minimum_license_severity"`
	FailOnCodeQualityScan                    bool    `json:"fail_on_code_quality_scan"`
	EnableCodeQualityScan                    bool    `json:"enable_code_quality_scan"`
	PostCodeQualityInlineCommentsMinSeverity string  `json:"post_code_quality_inline_comments_min_severity"`
	RunDeepAuditPRScan                       bool    `json:"run_deep_audit_pr_scan"`
}

func (r *defaultPRChecksSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_default_pr_checks_settings"
}

func (r *defaultPRChecksSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages the workspace default pull request checks configuration. " +
			"This configuration is applied to newly activated repositories that do not have a repo-specific configuration. " +
			"It does not update existing repo configurations. " +
			"There is exactly one default PR checks settings object per workspace. " +
			"When all checks are disabled or the resource is removed, the settings are deleted in Aikido for the workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace default PR checks settings identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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
					"Set to none to disable license scanning. High and critical are available on Pro or higher plans.",
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
					"Only available in the EU region.",
			},
		},
	}
}

func (r *defaultPRChecksSettingsResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var settings defaultPRChecksSettingsModel
	response.Diagnostics.Append(request.Config.Get(ctx, &settings)...)

	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(validateDefaultPRChecksSettingsCodeQuality(settings)...)
	response.Diagnostics.Append(validateDefaultPRChecksSettingsDeepAudit(settings)...)
}

func validateDefaultPRChecksSettingsCodeQuality(settings defaultPRChecksSettingsModel) diag.Diagnostics {
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

func validateDefaultPRChecksSettingsDeepAudit(settings defaultPRChecksSettingsModel) diag.Diagnostics {
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

func (r *defaultPRChecksSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *defaultPRChecksSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned defaultPRChecksSettingsModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setDefaultPRChecksSettings(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring default PR checks settings", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *defaultPRChecksSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var prior defaultPRChecksSettingsModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	apiSettings, err := getDefaultPRChecksSettings(ctx, r.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading default PR checks settings", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, mergeDefaultPRChecksSettingsAPIAndPrior(apiSettings, &prior))...)
}

func (r *defaultPRChecksSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned defaultPRChecksSettingsModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setDefaultPRChecksSettings(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring default PR checks settings", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *defaultPRChecksSettingsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var prior defaultPRChecksSettingsModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	minimumSeverity := prior.MinimumSeverity.ValueString()
	if minimumSeverity == "" {
		minimumSeverity = "critical"
	}

	body := map[string]any{
		"minimum_severity":          minimumSeverity,
		"fail_on_dependency_scan":   false,
		"fail_on_sast_scan":         false,
		"fail_on_iac_scan":          false,
		"fail_on_secrets_scan":      false,
		"fail_on_malware_scan":      false,
		"minimum_license_severity":  "none",
		"enable_code_quality_scan":  false,
		"fail_on_code_quality_scan": false,
		"run_deep_audit_pr_scan":    false,
	}

	if err := r.client.Do(ctx, "POST", defaultPRChecksSettingsPath, body, nil); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error removing default PR checks settings", err.Error())
	}
}

func (r *defaultPRChecksSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func (r *defaultPRChecksSettingsResource) setDefaultPRChecksSettings(ctx context.Context, planned defaultPRChecksSettingsModel) (defaultPRChecksSettingsModel, error) {
	if err := r.client.Do(ctx, "POST", defaultPRChecksSettingsPath, constructDefaultPRChecksSettingsBody(planned), nil); err != nil {
		return defaultPRChecksSettingsModel{}, err
	}

	apiSettings, err := getDefaultPRChecksSettings(ctx, r.client)
	if err != nil {
		return defaultPRChecksSettingsModel{}, err
	}

	requestedLicenseSeverity := planned.MinimumLicenseSeverity.ValueString()
	appliedLicenseSeverity := apiSettings.MinimumLicenseSeverity
	if appliedLicenseSeverity == "" {
		appliedLicenseSeverity = "none"
	}
	if requestedLicenseSeverity != appliedLicenseSeverity {
		return defaultPRChecksSettingsModel{}, fmt.Errorf(
			"minimum_license_severity %q was not applied; Aikido returned %q; license scanning is available on Pro or higher plans; set minimum_license_severity to \"none\" or contact Aikido",
			requestedLicenseSeverity,
			appliedLicenseSeverity,
		)
	}

	return mergeDefaultPRChecksSettingsAPIAndPrior(apiSettings, &planned), nil
}

func getDefaultPRChecksSettings(ctx context.Context, apiClient *client.Client) (defaultPRChecksSettingsAPI, error) {
	var settings defaultPRChecksSettingsAPI
	if err := apiClient.Do(ctx, "GET", defaultPRChecksSettingsPath, nil, &settings); err != nil {

		// No row means default PR checks are disabled / deleted for the workspace.
		if client.NotFound(err) {
			return defaultPRChecksSettingsAPI{IsEnabled: false}, nil
		}

		return defaultPRChecksSettingsAPI{}, err
	}

	return settings, nil
}

func constructDefaultPRChecksSettingsBody(planned defaultPRChecksSettingsModel) map[string]any {
	body := map[string]any{
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

func mapDefaultPRChecksSettingsAPIToModel(api defaultPRChecksSettingsAPI) defaultPRChecksSettingsModel {
	state := defaultPRChecksSettingsModel{
		ID:                                       types.StringValue(defaultPRChecksSettingsResourceID),
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
		PostInlineCommentsMinSeverity:            types.StringValue("none"),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
	}

	if api.PostInlineCommentsMinSeverity != nil && *api.PostInlineCommentsMinSeverity != "" {
		state.PostInlineCommentsMinSeverity = types.StringValue(*api.PostInlineCommentsMinSeverity)
	}

	if api.EnableCodeQualityScan &&
		api.PostCodeQualityInlineCommentsMinSeverity != "" &&
		api.PostCodeQualityInlineCommentsMinSeverity != "none" {
		state.PostCodeQualityInlineCommentsMinSeverity = types.StringValue(api.PostCodeQualityInlineCommentsMinSeverity)
	}

	if api.MinimumLicenseSeverity == "" {
		state.MinimumLicenseSeverity = types.StringValue("none")
	}

	return state
}

func mergeDefaultPRChecksSettingsAPIAndPrior(api defaultPRChecksSettingsAPI, prior *defaultPRChecksSettingsModel) defaultPRChecksSettingsModel {
	state := mapDefaultPRChecksSettingsAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.IsEnabled {
		state.MinimumSeverity = prior.MinimumSeverity
		state.PostInlineCommentsMinSeverity = prior.PostInlineCommentsMinSeverity
		state.PostCodeQualityInlineCommentsMinSeverity = prior.PostCodeQualityInlineCommentsMinSeverity
		return state
	}

	if !api.EnableCodeQualityScan {
		state.PostCodeQualityInlineCommentsMinSeverity = prior.PostCodeQualityInlineCommentsMinSeverity
	}

	return state
}
