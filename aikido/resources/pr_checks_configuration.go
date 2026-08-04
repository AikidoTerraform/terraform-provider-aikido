package resources

import (
	"context"
	"fmt"
	"net/url"
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

const prChecksConfigurationsPath = "/public/v1/repositories/code/continuous_integration/checks"

var (
	_ resource.Resource                   = &prChecksConfigurationResource{}
	_ resource.ResourceWithImportState    = &prChecksConfigurationResource{}
	_ resource.ResourceWithConfigure      = &prChecksConfigurationResource{}
	_ resource.ResourceWithValidateConfig = &prChecksConfigurationResource{}
)

func NewPRChecksConfigurationResource() resource.Resource {
	return &prChecksConfigurationResource{}
}

type prChecksConfigurationResource struct {
	client *client.Client
}

type prChecksConfigurationModel struct {
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
	PostDeepAuditInlineCommentsMinSeverity   types.String `tfsdk:"post_deep_audit_inline_comments_min_severity"`
}

type prChecksConfigurationAPI struct {
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
	PostDeepAuditInlineCommentsMinSeverity   string  `json:"post_deep_audit_inline_comments_min_severity"`
}

func (r *prChecksConfigurationResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_pr_checks_configuration"
}

func (r *prChecksConfigurationResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages pull request checks configuration for one Aikido code repository.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido PR checks configuration ID.",
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
				Description: "Minimum severity of new issues for when the CI check fails. One of: low, medium, high, critical.",
				Validators: []validator.String{
					stringvalidator.OneOf("low", "medium", "high", "critical"),
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
					"For bitbucket and azure_devops repositories this is currently only available in the EU region.",
			},
			"post_deep_audit_inline_comments_min_severity": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Minimum severity for Deep Review inline comments. " +
					"One of: none, low, medium, high, critical. " +
					"Ignored when run_deep_audit_pr_scan is false and may be omitted.",
				Validators: []validator.String{
					stringvalidator.OneOf("none", "low", "medium", "high", "critical"),
				},
			},
		},
	}
}

func (r *prChecksConfigurationResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var config prChecksConfigurationModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)

	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(validatePRChecksCodeQuality(config)...)
	response.Diagnostics.Append(validatePRChecksDeepAudit(config)...)
}

func validatePRChecksCodeQuality(config prChecksConfigurationModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if config.EnableCodeQualityScan.IsNull() || config.EnableCodeQualityScan.IsUnknown() {
		return diags
	}

	if config.EnableCodeQualityScan.ValueBool() {
		if config.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
			diags.AddAttributeError(
				path.Root("post_code_quality_inline_comments_min_severity"),
				"Missing post_code_quality_inline_comments_min_severity",
				`"post_code_quality_inline_comments_min_severity" is required when "enable_code_quality_scan" is true. Use one of: low, medium, high, critical.`,
			)
		}

		return diags
	}

	if !config.FailOnCodeQualityScan.IsNull() && !config.FailOnCodeQualityScan.IsUnknown() && config.FailOnCodeQualityScan.ValueBool() {
		diags.AddAttributeError(
			path.Root("fail_on_code_quality_scan"),
			"Invalid fail_on_code_quality_scan",
			`"fail_on_code_quality_scan" must be false when "enable_code_quality_scan" is false.`,
		)
	}

	return diags
}

func validatePRChecksDeepAudit(config prChecksConfigurationModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if config.RunDeepAuditPRScan.IsNull() || config.RunDeepAuditPRScan.IsUnknown() || !config.RunDeepAuditPRScan.ValueBool() {
		return diags
	}

	scanFlags := []types.Bool{
		config.FailOnDependencyScan,
		config.FailOnSastScan,
		config.FailOnIacScan,
		config.FailOnSecretsScan,
		config.FailOnMalwareScan,
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

	if !config.MinimumLicenseSeverity.IsUnknown() && !config.MinimumLicenseSeverity.IsNull() && config.MinimumLicenseSeverity.ValueString() != "none" {
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

func (r *prChecksConfigurationResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *prChecksConfigurationResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned prChecksConfigurationModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setPRChecksConfiguration(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring PR checks", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *prChecksConfigurationResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var prior prChecksConfigurationModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	apiConfig, err := getPRChecksConfiguration(ctx, r.client, prior.CodeRepoID.ValueInt64())
	if err != nil {
		response.Diagnostics.AddError("Error reading PR checks configuration", err.Error())
		return
	}

	if apiConfig == nil {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, mergePRChecksAPIAndPrior(*apiConfig, &prior))...)
}

func (r *prChecksConfigurationResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned prChecksConfigurationModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setPRChecksConfiguration(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring PR checks", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

// the API has no delete endpoint for PR checks configuration but is required by the resource interface.
func (r *prChecksConfigurationResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *prChecksConfigurationResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
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

func (r *prChecksConfigurationResource) setPRChecksConfiguration(ctx context.Context, planned prChecksConfigurationModel) (prChecksConfigurationModel, error) {
	if err := r.client.Do(ctx, "POST", prChecksConfigurationsPath, constructPRChecksBody(planned), nil); err != nil {
		return prChecksConfigurationModel{}, err
	}

	apiConfig, err := getPRChecksConfiguration(ctx, r.client, planned.CodeRepoID.ValueInt64())
	if err != nil {
		return prChecksConfigurationModel{}, err
	}

	if apiConfig == nil {
		return prChecksConfigurationModel{}, fmt.Errorf("configuration for code_repo_id %d was not found after update", planned.CodeRepoID.ValueInt64())
	}

	return mergePRChecksAPIAndPrior(*apiConfig, &planned), nil
}

func getPRChecksConfiguration(ctx context.Context, apiClient *client.Client, codeRepoID int64) (*prChecksConfigurationAPI, error) {
	url := prChecksConfigurationsPath + "?filter_code_repo_id=" + url.QueryEscape(strconv.FormatInt(codeRepoID, 10))

	var configs []prChecksConfigurationAPI
	if err := apiClient.Do(ctx, "GET", url, nil, &configs); err != nil {
		return nil, err
	}

	if len(configs) == 0 {
		return nil, nil
	}

	return &configs[0], nil
}

func constructPRChecksBody(planned prChecksConfigurationModel) map[string]any {
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

		if planned.RunDeepAuditPRScan.ValueBool() &&
			!planned.PostDeepAuditInlineCommentsMinSeverity.IsNull() &&
			!planned.PostDeepAuditInlineCommentsMinSeverity.IsUnknown() {
			body["post_deep_audit_inline_comments_min_severity"] = planned.PostDeepAuditInlineCommentsMinSeverity.ValueString()
		}
	}

	if !planned.PostInlineCommentsMinSeverity.IsNull() && !planned.PostInlineCommentsMinSeverity.IsUnknown() {
		body["post_inline_comments_min_severity"] = planned.PostInlineCommentsMinSeverity.ValueString()
	}

	return body
}

func mapPRChecksAPIToModel(api prChecksConfigurationAPI) prChecksConfigurationModel {
	state := prChecksConfigurationModel{
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
		PostDeepAuditInlineCommentsMinSeverity:   types.StringValue(api.PostDeepAuditInlineCommentsMinSeverity),
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

	if api.PostDeepAuditInlineCommentsMinSeverity == "" {
		state.PostDeepAuditInlineCommentsMinSeverity = types.StringNull()
	}

	return state
}

// mergePRChecksAPIAndPrior returns state from the API, but mirrors prior values for
// fields the API ignores when the related feature is disabled.
func mergePRChecksAPIAndPrior(api prChecksConfigurationAPI, prior *prChecksConfigurationModel) prChecksConfigurationModel {
	state := mapPRChecksAPIToModel(api)
	if prior == nil {
		return state
	}

	if !api.EnableCodeQualityScan {
		state.PostCodeQualityInlineCommentsMinSeverity = prior.PostCodeQualityInlineCommentsMinSeverity
	}

	if !api.RunDeepAuditPRScan {
		state.PostDeepAuditInlineCommentsMinSeverity = prior.PostDeepAuditInlineCommentsMinSeverity
	}

	return state
}
