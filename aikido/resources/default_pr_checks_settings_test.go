package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestConstructDefaultPRChecksBody(t *testing.T) {
	model := defaultPRChecksSettingsModel{
		MinimumSeverity:                          types.StringValue("high"),
		FailOnDependencyScan:                     types.BoolValue(true),
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(true),
		FailOnMalwareScan:                        types.BoolValue(false),
		PostInlineCommentsMinSeverity:            types.StringValue("critical"),
		MinimumLicenseSeverity:                   types.StringValue("high"),
		FailOnCodeQualityScan:                    types.BoolValue(true),
		EnableCodeQualityScan:                    types.BoolValue(true),
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("medium"),
		RunDeepAuditPRScan:                       types.BoolValue(true),
	}

	body := constructDefaultPRChecksSettingsBody(model)
	if _, ok := body["code_repo_id"]; ok {
		t.Fatalf("did not expect code_repo_id in default body")
	}
	if body["post_inline_comments_min_severity"] != "critical" {
		t.Fatalf("unexpected post_inline_comments_min_severity: %#v", body["post_inline_comments_min_severity"])
	}
	if body["post_code_quality_inline_comments_min_severity"] != "medium" {
		t.Fatalf("unexpected post_code_quality_inline_comments_min_severity: %#v", body["post_code_quality_inline_comments_min_severity"])
	}
	if body["run_deep_audit_pr_scan"] != true {
		t.Fatalf("unexpected run_deep_audit_pr_scan: %#v", body["run_deep_audit_pr_scan"])
	}
}

func TestConstructDefaultPRChecksBody_OmitsOptionalNullFields(t *testing.T) {
	model := defaultPRChecksSettingsModel{
		MinimumSeverity:                          types.StringValue("high"),
		FailOnDependencyScan:                     types.BoolValue(true),
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(true),
		FailOnSecretsScan:                        types.BoolValue(true),
		FailOnMalwareScan:                        types.BoolValue(true),
		PostInlineCommentsMinSeverity:            types.StringNull(),
		MinimumLicenseSeverity:                   types.StringValue("high"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
		RunDeepAuditPRScan:                       types.BoolNull(),
	}

	body := constructDefaultPRChecksSettingsBody(model)
	for _, key := range []string{
		"post_inline_comments_min_severity",
		"post_code_quality_inline_comments_min_severity",
		"run_deep_audit_pr_scan",
	} {
		if _, ok := body[key]; ok {
			t.Fatalf("did not expect %s in body", key)
		}
	}
}

func TestConstructDefaultPRChecksBody_DeepAuditDisabledOmitsInlineSeverity(t *testing.T) {
	model := defaultPRChecksSettingsModel{
		MinimumSeverity:        types.StringValue("high"),
		FailOnDependencyScan:   types.BoolValue(true),
		FailOnSastScan:         types.BoolValue(true),
		FailOnIacScan:          types.BoolValue(true),
		FailOnSecretsScan:      types.BoolValue(true),
		FailOnMalwareScan:      types.BoolValue(true),
		MinimumLicenseSeverity: types.StringValue("none"),
		FailOnCodeQualityScan:  types.BoolValue(false),
		EnableCodeQualityScan:  types.BoolValue(false),
		RunDeepAuditPRScan:     types.BoolValue(false),
	}

	body := constructDefaultPRChecksSettingsBody(model)
	if body["run_deep_audit_pr_scan"] != false {
		t.Fatalf("unexpected run_deep_audit_pr_scan: %#v", body["run_deep_audit_pr_scan"])
	}
}

func validateDefaultPRChecksSettings(settings defaultPRChecksSettingsModel) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(validateDefaultPRChecksSettingsCodeQuality(settings)...)
	diags.Append(validateDefaultPRChecksSettingsDeepAudit(settings)...)
	return diags
}

func TestValidateDefaultPRChecksSettings(t *testing.T) {
	validBase := defaultPRChecksSettingsModel{
		MinimumSeverity:                          types.StringValue("high"),
		FailOnDependencyScan:                     types.BoolValue(true),
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(true),
		FailOnSecretsScan:                        types.BoolValue(true),
		FailOnMalwareScan:                        types.BoolValue(true),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(true),
		EnableCodeQualityScan:                    types.BoolValue(true),
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("medium"),
		RunDeepAuditPRScan:                       types.BoolValue(false),
	}

	tests := []struct {
		name      string
		mutate    func(*defaultPRChecksSettingsModel)
		wantError bool
	}{
		{name: "valid settings"},
		{
			name: "code quality enabled rejects omitted inline severity",
			mutate: func(m *defaultPRChecksSettingsModel) {
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "code quality disabled rejects fail_on_code_quality_scan",
			mutate: func(m *defaultPRChecksSettingsModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(true)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "deep audit requires a vulnerability scan",
			mutate: func(m *defaultPRChecksSettingsModel) {
				m.FailOnDependencyScan = types.BoolValue(false)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolValue(true)
			},
			wantError: true,
		},
		{
			name: "deep audit allowed with dependency scan",
			mutate: func(m *defaultPRChecksSettingsModel) {
				m.FailOnDependencyScan = types.BoolValue(true)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolValue(true)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := validBase
			if tc.mutate != nil {
				tc.mutate(&settings)
			}
			diags := validateDefaultPRChecksSettings(settings)
			if diags.HasError() != tc.wantError {
				t.Errorf("HasError() = %v, want %v (diags: %v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestMapDefaultPRChecksAPIToModel_NormalizesGetDefaults(t *testing.T) {
	api := defaultPRChecksSettingsAPI{
		IsEnabled:                                true,
		MinimumSeverity:                          "high",
		FailOnDependencyScan:                     true,
		FailOnSastScan:                           true,
		FailOnIacScan:                            true,
		FailOnSecretsScan:                        true,
		FailOnMalwareScan:                        true,
		PostInlineCommentsMinSeverity:            nil,
		MinimumLicenseSeverity:                   "",
		FailOnCodeQualityScan:                    false,
		EnableCodeQualityScan:                    false,
		PostCodeQualityInlineCommentsMinSeverity: "none",
		RunDeepAuditPRScan:                       false,
	}

	state := mapDefaultPRChecksSettingsAPIToModel(api)
	if state.ID.ValueString() != defaultPRChecksSettingsResourceID {
		t.Fatalf("id = %q, want %s", state.ID.ValueString(), defaultPRChecksSettingsResourceID)
	}
	if state.PostInlineCommentsMinSeverity.ValueString() != "none" {
		t.Fatalf("post_inline_comments_min_severity = %q, want none", state.PostInlineCommentsMinSeverity.ValueString())
	}
	if !state.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %#v, want null", state.PostCodeQualityInlineCommentsMinSeverity)
	}
	if state.MinimumLicenseSeverity.ValueString() != "none" {
		t.Fatalf("minimum_license_severity = %q, want none", state.MinimumLicenseSeverity.ValueString())
	}
}

func TestMapDefaultPRChecksAPIToModel_IgnoresDeepAuditUIDefaultWhenDisabled(t *testing.T) {
	api := defaultPRChecksSettingsAPI{
		IsEnabled:                                false,
		MinimumSeverity:                          "critical",
		FailOnDependencyScan:                     false,
		FailOnSastScan:                           false,
		FailOnIacScan:                            false,
		FailOnSecretsScan:                        false,
		FailOnMalwareScan:                        false,
		MinimumLicenseSeverity:                   "none",
		EnableCodeQualityScan:                    false,
		PostCodeQualityInlineCommentsMinSeverity: "none",
		RunDeepAuditPRScan:                       false,
	}

	state := mapDefaultPRChecksSettingsAPIToModel(api)
	if state.FailOnDependencyScan.ValueBool() {
		t.Fatalf("fail_on_dependency_scan = true, want false")
	}
	if !state.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %#v, want null", state.PostCodeQualityInlineCommentsMinSeverity)
	}
}

func TestMapDefaultPRChecksAPIToModel_CodeQualityEnabled(t *testing.T) {
	inline := "critical"
	api := defaultPRChecksSettingsAPI{
		IsEnabled:                                true,
		MinimumSeverity:                          "high",
		FailOnDependencyScan:                     true,
		FailOnSastScan:                           true,
		FailOnIacScan:                            true,
		FailOnSecretsScan:                        true,
		FailOnMalwareScan:                        true,
		PostInlineCommentsMinSeverity:            &inline,
		MinimumLicenseSeverity:                   "high",
		FailOnCodeQualityScan:                    true,
		EnableCodeQualityScan:                    true,
		PostCodeQualityInlineCommentsMinSeverity: "medium",
		RunDeepAuditPRScan:                       true,
	}

	state := mapDefaultPRChecksSettingsAPIToModel(api)
	if state.PostInlineCommentsMinSeverity.ValueString() != "critical" {
		t.Fatalf("post_inline_comments_min_severity = %q, want critical", state.PostInlineCommentsMinSeverity.ValueString())
	}
	if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %q, want medium", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
	}
}

func TestMergeDefaultPRChecksAPIAndPrior_PreservesIgnoredFields(t *testing.T) {
	api := defaultPRChecksSettingsAPI{
		IsEnabled:                                true,
		MinimumSeverity:                          "high",
		FailOnDependencyScan:                     true,
		FailOnSastScan:                           true,
		FailOnIacScan:                            true,
		FailOnSecretsScan:                        true,
		FailOnMalwareScan:                        true,
		MinimumLicenseSeverity:                   "none",
		FailOnCodeQualityScan:                    false,
		EnableCodeQualityScan:                    false,
		PostCodeQualityInlineCommentsMinSeverity: "none",
		RunDeepAuditPRScan:                       false,
	}
	prior := defaultPRChecksSettingsModel{
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("medium"),
	}

	state := mergeDefaultPRChecksSettingsAPIAndPrior(api, &prior)
	if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %q, want medium from prior", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
	}
}

func TestMergeDefaultPRChecksAPIAndPrior_DeletedRowKeepsSeverityPlaceholders(t *testing.T) {
	// GET payload when the default config row was deleted (all checks off).
	// Scan flags from GET are authoritative; severity placeholders keep prior.
	api := defaultPRChecksSettingsAPI{
		IsEnabled:                                false,
		MinimumSeverity:                          "critical",
		FailOnDependencyScan:                     false,
		FailOnSastScan:                           false,
		FailOnIacScan:                            false,
		FailOnSecretsScan:                        false,
		FailOnMalwareScan:                        false,
		PostInlineCommentsMinSeverity:            nil,
		MinimumLicenseSeverity:                   "none",
		FailOnCodeQualityScan:                    false,
		EnableCodeQualityScan:                    false,
		PostCodeQualityInlineCommentsMinSeverity: "none",
		RunDeepAuditPRScan:                       false,
	}
	prior := defaultPRChecksSettingsModel{
		ID:                                       types.StringValue(defaultPRChecksSettingsResourceID),
		MinimumSeverity:                          types.StringValue("high"),
		FailOnDependencyScan:                     types.BoolValue(true), // stale vs remote disabled
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(false),
		FailOnMalwareScan:                        types.BoolValue(false),
		PostInlineCommentsMinSeverity:            types.StringValue("critical"),
		MinimumLicenseSeverity:                   types.StringValue("high"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("medium"),
		RunDeepAuditPRScan:                       types.BoolValue(false),
	}

	state := mergeDefaultPRChecksSettingsAPIAndPrior(api, &prior)
	if state.MinimumSeverity.ValueString() != "high" {
		t.Fatalf("minimum_severity = %q, want high from prior", state.MinimumSeverity.ValueString())
	}
	if state.FailOnDependencyScan.ValueBool() {
		t.Fatalf("fail_on_dependency_scan = true, want false from GET")
	}
	if state.FailOnSastScan.ValueBool() {
		t.Fatalf("fail_on_sast_scan = true, want false from GET")
	}
	if state.MinimumLicenseSeverity.ValueString() != "none" {
		t.Fatalf("minimum_license_severity = %q, want none from GET", state.MinimumLicenseSeverity.ValueString())
	}
	if state.PostInlineCommentsMinSeverity.ValueString() != "critical" {
		t.Fatalf("post_inline_comments_min_severity = %q, want critical from prior", state.PostInlineCommentsMinSeverity.ValueString())
	}
	if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %q, want medium from prior", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
	}
}

func TestMergeDefaultPRChecksAPIAndPrior_DeletedRowWithoutPriorMapsGet(t *testing.T) {
	api := defaultPRChecksSettingsAPI{
		IsEnabled:                                false,
		MinimumSeverity:                          "critical",
		FailOnDependencyScan:                     false,
		FailOnSastScan:                           false,
		FailOnIacScan:                            false,
		FailOnSecretsScan:                        false,
		FailOnMalwareScan:                        false,
		PostInlineCommentsMinSeverity:            nil,
		MinimumLicenseSeverity:                   "none",
		FailOnCodeQualityScan:                    false,
		EnableCodeQualityScan:                    false,
		PostCodeQualityInlineCommentsMinSeverity: "none",
		RunDeepAuditPRScan:                       false,
	}

	state := mergeDefaultPRChecksSettingsAPIAndPrior(api, nil)
	if state.MinimumSeverity.ValueString() != "critical" {
		t.Fatalf("minimum_severity = %q, want critical", state.MinimumSeverity.ValueString())
	}
	if state.FailOnDependencyScan.ValueBool() {
		t.Fatalf("fail_on_dependency_scan = true, want false")
	}
	if state.MinimumLicenseSeverity.ValueString() != "none" {
		t.Fatalf("minimum_license_severity = %q, want none", state.MinimumLicenseSeverity.ValueString())
	}
	if state.PostInlineCommentsMinSeverity.ValueString() != "none" {
		t.Fatalf("post_inline_comments_min_severity = %q, want none", state.PostInlineCommentsMinSeverity.ValueString())
	}
	if !state.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %#v, want null", state.PostCodeQualityInlineCommentsMinSeverity)
	}
}
