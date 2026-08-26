package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/helpers"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
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
	allPrChecksSettingsPath           = "/public/v1/repositories/code/continuous_integration/checks/all"
	allRepoPRChecksSettingsResourceID = "all_repo_pr_checks_settings"
)

var (
	_ resource.Resource                   = &allPrChecksSettingsResource{}
	_ resource.ResourceWithImportState    = &allPrChecksSettingsResource{}
	_ resource.ResourceWithConfigure      = &allPrChecksSettingsResource{}
	_ resource.ResourceWithValidateConfig = &allPrChecksSettingsResource{}
)

func NewAllRepoPRChecksSettingsResource() resource.Resource {
	return &allPrChecksSettingsResource{}
}

type allPrChecksSettingsResource struct {
	client *client.Client
}

type allPrChecksSettingsModel struct {
	ID                                       types.String `tfsdk:"id"`
	ExcludedRepos                            []int64      `tfsdk:"excluded_repos"`
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

func (r *allPrChecksSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_all_repo_pr_checks_settings"
}

func (r *allPrChecksSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Applies pull request checks settings to every active GitHub repository. " +
			"Currently only GitHub is supported. " +
			"Use excluded_repos to skip repositories that should keep their current settings or need specific settings. " +
			"There is exactly one all-repos PR checks settings resource per workspace. " +
			"The Aikido API has no delete endpoint for PR checks settings, so destroying this resource " +
			"only removes it from Terraform state and leaves the remote settings unchanged.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace all-repos PR checks settings identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"excluded_repos": schema.SetAttribute{
				Optional:    true,
				ElementType: types.Int64Type,
				Description: "Repository IDs to exclude from the bulk configuration. " +
					"Omitted or empty applies the settings to every active GitHub repository. " +
					"Persisted by Aikido and recovered on import and refresh.",
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
					"or minimum_license_severity other than none).",
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

func (r *allPrChecksSettingsResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var settings allPrChecksSettingsModel
	response.Diagnostics.Append(request.Config.Get(ctx, &settings)...)

	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(validateAllPRChecksSettingsCodeQuality(settings)...)
	response.Diagnostics.Append(validateAllPRChecksSettingsDeepAudit(settings)...)
}

func validateAllPRChecksSettingsCodeQuality(settings allPrChecksSettingsModel) diag.Diagnostics {
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

func validateAllPRChecksSettingsDeepAudit(settings allPrChecksSettingsModel) diag.Diagnostics {
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

func (r *allPrChecksSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *allPrChecksSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned allPrChecksSettingsModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setAllPRChecksSettings(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring PR checks settings for all existing repositories", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *allPrChecksSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var prior allPrChecksSettingsModel
	response.Diagnostics.Append(request.State.Get(ctx, &prior)...)

	if response.Diagnostics.HasError() {
		return
	}

	excludedRepos, err := getAllPRChecksExcludedRepos(ctx, r.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading all-repos PR checks exclusions", err.Error())
		return
	}

	// Prefer the server-side exclusion list so import and refresh recover it.
	prior.ExcludedRepos = excludedRepos

	settingsList, err := prChecksSettingsListFromCache(ctx, r.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading PR checks settings list", err.Error())
		return
	}

	repos, err := repositories.All(ctx, r.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading repositories list", err.Error())
		return
	}

	// get the actual repositories to update. This are the github active repos minus the excluded repos and Aikido-internal repositories.
	actualReposToUpdate := getActualReposToUpdate(repos, prior.ExcludedRepos)

	// get the settings for those actual repositories to update. If no drift, return the prior state. If drift, return the settings for the drifted repository.
	settings := keepAllPRChecksSettingsUnlessDrifted(settingsList, actualReposToUpdate, prior)

	// On import, state only has id. If we could not load settings from any
	// managed repo, refuse instead of saving empty required attributes.
	if (prior.MinimumSeverity.IsNull() || prior.MinimumSeverity.IsUnknown()) &&
		settings.MinimumSeverity.ValueString() == "" {
		response.Diagnostics.AddError(
			"Cannot read all-repos PR checks settings",
			"No active GitHub repository managed by this resource has PR checks settings. "+
				"Import and refresh need at least one non-excluded active GitHub repository with PR checks settings, "+
				"or an existing Terraform state that already contains the settings.",
		)
		return
	}

	// set the state
	response.Diagnostics.Append(response.State.Set(ctx, settings)...)
}

func (r *allPrChecksSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned allPrChecksSettingsModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)

	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setAllPRChecksSettings(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring PR checks settings for all existing repositories", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

// Delete is a no-op: the Aikido API has no delete endpoint for PR checks settings.
// Destroy only removes the resource from Terraform state; remote settings are left unchanged.
func (r *allPrChecksSettingsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *allPrChecksSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	if request.ID != allRepoPRChecksSettingsResourceID {
		response.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected %q, got %q.", allRepoPRChecksSettingsResourceID, request.ID),
		)
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), allRepoPRChecksSettingsResourceID)...)
}

// allPrChecksBulkExcludedReposAPI is the GET response for workspace-level bulk PR-checks exclusions.
type allPrChecksBulkExcludedReposAPI struct {
	ExcludedRepos []int64 `json:"excluded_repos"`
}

// getAllPRChecksExcludedRepos loads exclusions for Read/import refresh.
func getAllPRChecksExcludedRepos(ctx context.Context, apiClient *client.Client) ([]int64, error) {
	var api allPrChecksBulkExcludedReposAPI
	if err := apiClient.Do(ctx, "GET", allPrChecksSettingsPath+"/excluded_repos", nil, &api); err != nil {
		if client.NotFound(err) {
			return []int64{}, nil
		}

		return nil, err
	}

	return helpers.NormalizeIDs(api.ExcludedRepos), nil
}

func (r *allPrChecksSettingsResource) setAllPRChecksSettings(ctx context.Context, planned allPrChecksSettingsModel) (allPrChecksSettingsModel, error) {
	if err := r.client.Do(ctx, "POST", allPrChecksSettingsPath, constructAllPRChecksSettingsBody(planned), nil); err != nil {
		return allPrChecksSettingsModel{}, err
	}

	// Drop the pre-write list so the next load paginates post-write settings
	// into the shared cache (page size 100 — not one GET per repository).
	client.InvalidateCached(r.client, prChecksSettingsCacheKey)

	return allPRChecksSettingsStateFromPlan(planned), nil
}

func constructAllPRChecksSettingsBody(planned allPrChecksSettingsModel) map[string]any {
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
		"excluded_repos":            helpers.NormalizeIDs(planned.ExcludedRepos),
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

// getActualReposToUpdate is the set the bulk endpoint actually writes:
// active GitHub repositories, minus excluded_repos and Aikido-internal repositories.
func getActualReposToUpdate(repos []repositories.Repository, excludedRepos []int64) map[int64]struct{} {
	skip := make(map[int64]struct{}, len(excludedRepos))
	for _, id := range excludedRepos {
		skip[id] = struct{}{}
	}

	actualReposToUpdate := make(map[int64]struct{})
	for _, repo := range repos {
		shouldAlsoIgnoreAikidoInternalRepo := strings.Contains(repo.ExternalRepoID, "_aikidoclone_") || strings.Contains(repo.ExternalRepoID, "custom_") || strings.Contains(repo.ExternalRepoID, "selfscan_")

		// Only native GitHub repositories are supported
		if !repo.Active || repo.Provider != "github" || shouldAlsoIgnoreAikidoInternalRepo {
			continue
		}

		if _, excluded := skip[repo.ID]; excluded {
			continue
		}

		actualReposToUpdate[repo.ID] = struct{}{}
	}

	return actualReposToUpdate
}

// keepAllPRChecksSettingsUnlessDrifted returns prior when every managed repo
// still matches Terraform. If any repo drifted, it returns that repo's settings
// so the next plan shows the difference. Missing PR-checks rows are drift.
func keepAllPRChecksSettingsUnlessDrifted(settings map[int64]prChecksSettingsAPI, reposToUpdate map[int64]struct{}, prior allPrChecksSettingsModel) allPrChecksSettingsModel {
	var chosen *prChecksSettingsAPI
	drifted := false

	for id := range reposToUpdate {
		row, exists := settings[id]
		if !exists {
			drifted = true
			continue
		}

		settings := allPRChecksSettingsFromAPI(row, &prior)
		if allPRChecksSettingsEqual(settings, prior) {
			continue
		}

		drifted = true
		if chosen == nil || row.CodeRepoID < chosen.CodeRepoID {
			chosen = &row
		}
	}

	if !drifted {
		return prior
	}

	if chosen != nil {
		return allPRChecksSettingsFromAPI(*chosen, &prior)
	}

	return allPRChecksSettingsFromAPI(prChecksSettingsAPI{}, &prior)
}

func allPRChecksSettingsEqual(currentSettings, previousSettings allPrChecksSettingsModel) bool {
	return currentSettings.MinimumSeverity.Equal(previousSettings.MinimumSeverity) &&
		currentSettings.FailOnDependencyScan.Equal(previousSettings.FailOnDependencyScan) &&
		currentSettings.FailOnSastScan.Equal(previousSettings.FailOnSastScan) &&
		currentSettings.FailOnIacScan.Equal(previousSettings.FailOnIacScan) &&
		currentSettings.FailOnSecretsScan.Equal(previousSettings.FailOnSecretsScan) &&
		currentSettings.FailOnMalwareScan.Equal(previousSettings.FailOnMalwareScan) &&
		currentSettings.PostInlineCommentsMinSeverity.Equal(previousSettings.PostInlineCommentsMinSeverity) &&
		currentSettings.MinimumLicenseSeverity.Equal(previousSettings.MinimumLicenseSeverity) &&
		currentSettings.FailOnCodeQualityScan.Equal(previousSettings.FailOnCodeQualityScan) &&
		currentSettings.EnableCodeQualityScan.Equal(previousSettings.EnableCodeQualityScan) &&
		currentSettings.PostCodeQualityInlineCommentsMinSeverity.Equal(previousSettings.PostCodeQualityInlineCommentsMinSeverity) &&
		currentSettings.RunDeepAuditPRScan.Equal(previousSettings.RunDeepAuditPRScan) &&
		currentSettings.PostDeepAuditInlineCommentsMinSeverity.Equal(previousSettings.PostDeepAuditInlineCommentsMinSeverity)
}

func allPRChecksSettingsFromAPI(api prChecksSettingsAPI, prior *allPrChecksSettingsModel) allPrChecksSettingsModel {
	var perRepoPrior *prChecksSettingsModel
	if prior != nil {
		perRepoPrior = &prChecksSettingsModel{
			PostCodeQualityInlineCommentsMinSeverity: prior.PostCodeQualityInlineCommentsMinSeverity,
			PostDeepAuditInlineCommentsMinSeverity:   prior.PostDeepAuditInlineCommentsMinSeverity,
		}
	}

	merged := mergePRChecksSettingsAPIAndPrior(api, perRepoPrior)
	state := allPrChecksSettingsModel{
		ID:                                       types.StringValue(allRepoPRChecksSettingsResourceID),
		MinimumSeverity:                          merged.MinimumSeverity,
		FailOnDependencyScan:                     merged.FailOnDependencyScan,
		FailOnSastScan:                           merged.FailOnSastScan,
		FailOnIacScan:                            merged.FailOnIacScan,
		FailOnSecretsScan:                        merged.FailOnSecretsScan,
		FailOnMalwareScan:                        merged.FailOnMalwareScan,
		PostInlineCommentsMinSeverity:            merged.PostInlineCommentsMinSeverity,
		MinimumLicenseSeverity:                   merged.MinimumLicenseSeverity,
		FailOnCodeQualityScan:                    merged.FailOnCodeQualityScan,
		EnableCodeQualityScan:                    merged.EnableCodeQualityScan,
		PostCodeQualityInlineCommentsMinSeverity: merged.PostCodeQualityInlineCommentsMinSeverity,
		RunDeepAuditPRScan:                       merged.RunDeepAuditPRScan,
		PostDeepAuditInlineCommentsMinSeverity:   merged.PostDeepAuditInlineCommentsMinSeverity,
	}

	if prior != nil {
		state.ExcludedRepos = prior.ExcludedRepos
	}

	return state
}

// allPRChecksSettingsStateFromPlan copies the plan into state after a successful POST.
func allPRChecksSettingsStateFromPlan(planned allPrChecksSettingsModel) allPrChecksSettingsModel {
	state := planned
	state.ID = types.StringValue(allRepoPRChecksSettingsResourceID)

	if planned.PostInlineCommentsMinSeverity.IsNull() || planned.PostInlineCommentsMinSeverity.IsUnknown() {
		state.PostInlineCommentsMinSeverity = types.StringValue("none")
	}

	if planned.RunDeepAuditPRScan.IsNull() || planned.RunDeepAuditPRScan.IsUnknown() {
		state.RunDeepAuditPRScan = types.BoolValue(false)
	}

	if !state.EnableCodeQualityScan.ValueBool() && planned.PostCodeQualityInlineCommentsMinSeverity.IsUnknown() {
		state.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
	}

	if planned.PostDeepAuditInlineCommentsMinSeverity.IsNull() || planned.PostDeepAuditInlineCommentsMinSeverity.IsUnknown() {
		if state.RunDeepAuditPRScan.ValueBool() {
			state.PostDeepAuditInlineCommentsMinSeverity = types.StringValue("none")
		} else {
			state.PostDeepAuditInlineCommentsMinSeverity = types.StringNull()
		}
	}

	return state
}
