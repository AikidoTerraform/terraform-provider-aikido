package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestConstructPRChecksBody(t *testing.T) {
	model := prChecksConfigurationModel{
		CodeRepoID:                               types.Int64Value(12),
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
		PostDeepAuditInlineCommentsMinSeverity:   types.StringValue("high"),
	}

	body := constructPRChecksBody(model)
	if body["code_repo_id"] != int64(12) {
		t.Fatalf("unexpected code_repo_id: %#v", body["code_repo_id"])
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
	if body["post_deep_audit_inline_comments_min_severity"] != "high" {
		t.Fatalf("unexpected post_deep_audit_inline_comments_min_severity: %#v", body["post_deep_audit_inline_comments_min_severity"])
	}
}

func TestConstructPRChecksBody_OmitsOptionalNullFields(t *testing.T) {
	model := prChecksConfigurationModel{
		CodeRepoID:                               types.Int64Value(12),
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
		PostDeepAuditInlineCommentsMinSeverity:   types.StringNull(),
	}

	body := constructPRChecksBody(model)
	for _, key := range []string{
		"post_inline_comments_min_severity",
		"post_code_quality_inline_comments_min_severity",
		"run_deep_audit_pr_scan",
		"post_deep_audit_inline_comments_min_severity",
	} {
		if _, ok := body[key]; ok {
			t.Fatalf("did not expect %s in body", key)
		}
	}
}

func TestConstructPRChecksBody_SendsInlineNoneAsString(t *testing.T) {
	model := prChecksConfigurationModel{
		CodeRepoID:                               types.Int64Value(12),
		MinimumSeverity:                          types.StringValue("high"),
		FailOnDependencyScan:                     types.BoolValue(true),
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(true),
		FailOnSecretsScan:                        types.BoolValue(true),
		FailOnMalwareScan:                        types.BoolValue(true),
		PostInlineCommentsMinSeverity:            types.StringValue("none"),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
	}

	body := constructPRChecksBody(model)
	if body["post_inline_comments_min_severity"] != "none" {
		t.Fatalf("unexpected post_inline_comments_min_severity: %#v", body["post_inline_comments_min_severity"])
	}
	if _, ok := body["post_code_quality_inline_comments_min_severity"]; ok {
		t.Fatalf("did not expect post_code_quality_inline_comments_min_severity in body")
	}
	if body["minimum_license_severity"] != "none" {
		t.Fatalf("unexpected minimum_license_severity: %#v", body["minimum_license_severity"])
	}
}

func TestConstructPRChecksBody_DeepAuditDisabledOmitsInlineSeverity(t *testing.T) {
	model := prChecksConfigurationModel{
		CodeRepoID:                             types.Int64Value(12),
		MinimumSeverity:                        types.StringValue("high"),
		FailOnDependencyScan:                   types.BoolValue(true),
		FailOnSastScan:                         types.BoolValue(true),
		FailOnIacScan:                          types.BoolValue(true),
		FailOnSecretsScan:                      types.BoolValue(true),
		FailOnMalwareScan:                      types.BoolValue(true),
		MinimumLicenseSeverity:                 types.StringValue("none"),
		FailOnCodeQualityScan:                  types.BoolValue(false),
		EnableCodeQualityScan:                  types.BoolValue(false),
		RunDeepAuditPRScan:                     types.BoolValue(false),
		PostDeepAuditInlineCommentsMinSeverity: types.StringValue("high"),
	}

	body := constructPRChecksBody(model)
	if body["run_deep_audit_pr_scan"] != false {
		t.Fatalf("unexpected run_deep_audit_pr_scan: %#v", body["run_deep_audit_pr_scan"])
	}
	if _, ok := body["post_deep_audit_inline_comments_min_severity"]; ok {
		t.Fatalf("did not expect post_deep_audit_inline_comments_min_severity in body when deep audit is disabled")
	}
}

func validatePRChecksConfig(config prChecksConfigurationModel) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(validatePRChecksCodeQuality(config)...)
	diags.Append(validatePRChecksDeepAudit(config)...)
	return diags
}

func TestValidatePRChecksConfig(t *testing.T) {
	validBase := prChecksConfigurationModel{
		CodeRepoID:                               types.Int64Value(12),
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
		PostDeepAuditInlineCommentsMinSeverity:   types.StringNull(),
	}

	tests := []struct {
		name      string
		mutate    func(*prChecksConfigurationModel)
		wantError bool
	}{
		{name: "valid config"},
		{
			name: "code quality enabled rejects omitted inline severity",
			mutate: func(m *prChecksConfigurationModel) {
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "code quality disabled rejects fail_on_code_quality_scan",
			mutate: func(m *prChecksConfigurationModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(true)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "code quality disabled allows set inline severity",
			mutate: func(m *prChecksConfigurationModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(false)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringValue("low")
			},
		},
		{
			name: "code quality disabled allows omitted inline severity",
			mutate: func(m *prChecksConfigurationModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(false)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
		},
		{
			name: "deep audit requires a vulnerability scan",
			mutate: func(m *prChecksConfigurationModel) {
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
			mutate: func(m *prChecksConfigurationModel) {
				m.FailOnDependencyScan = types.BoolValue(true)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolValue(true)
			},
		},
		{
			name: "deep audit allowed with license severity",
			mutate: func(m *prChecksConfigurationModel) {
				m.FailOnDependencyScan = types.BoolValue(false)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("high")
				m.RunDeepAuditPRScan = types.BoolValue(true)
			},
		},
		{
			name: "deep audit unknown is skipped",
			mutate: func(m *prChecksConfigurationModel) {
				m.FailOnDependencyScan = types.BoolValue(false)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolUnknown()
			},
		},
		{
			name: "deep audit null is skipped",
			mutate: func(m *prChecksConfigurationModel) {
				m.FailOnDependencyScan = types.BoolValue(false)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolNull()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := validBase
			if tc.mutate != nil {
				tc.mutate(&config)
			}
			diags := validatePRChecksConfig(config)
			if diags.HasError() != tc.wantError {
				t.Errorf("HasError() = %v, want %v (diags: %v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestMapPRChecksAPIToModel_NormalizesGetDefaults(t *testing.T) {
	api := prChecksConfigurationAPI{
		ID:                                       1,
		CodeRepoID:                               123,
		MinimumSeverity:                          "high",
		FailOnDependencyScan:                     true,
		FailOnSastScan:                           true,
		FailOnIacScan:                            true,
		FailOnSecretsScan:                        true,
		FailOnMalwareScan:                        true,
		PostInlineCommentsMinSeverity:            "",
		MinimumLicenseSeverity:                   "",
		FailOnCodeQualityScan:                    false,
		EnableCodeQualityScan:                    false,
		PostCodeQualityInlineCommentsMinSeverity: nil,
		RunDeepAuditPRScan:                       false,
		PostDeepAuditInlineCommentsMinSeverity:   "",
	}

	state := mapPRChecksAPIToModel(api)
	if state.PostInlineCommentsMinSeverity.ValueString() != "none" {
		t.Fatalf("post_inline_comments_min_severity = %q, want none", state.PostInlineCommentsMinSeverity.ValueString())
	}
	if !state.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %#v, want null", state.PostCodeQualityInlineCommentsMinSeverity)
	}
	if state.MinimumLicenseSeverity.ValueString() != "none" {
		t.Fatalf("minimum_license_severity = %q, want none", state.MinimumLicenseSeverity.ValueString())
	}
	if !state.PostDeepAuditInlineCommentsMinSeverity.IsNull() {
		t.Fatalf("post_deep_audit_inline_comments_min_severity = %#v, want null", state.PostDeepAuditInlineCommentsMinSeverity)
	}
}

func TestMapPRChecksAPIToModel_CodeQualityEnabled(t *testing.T) {
	severity := "medium"
	api := prChecksConfigurationAPI{
		ID:                                       1,
		CodeRepoID:                               123,
		MinimumSeverity:                          "high",
		FailOnDependencyScan:                     true,
		FailOnSastScan:                           true,
		FailOnIacScan:                            true,
		FailOnSecretsScan:                        true,
		FailOnMalwareScan:                        true,
		PostInlineCommentsMinSeverity:            "low",
		MinimumLicenseSeverity:                   "high",
		FailOnCodeQualityScan:                    true,
		EnableCodeQualityScan:                    true,
		PostCodeQualityInlineCommentsMinSeverity: &severity,
		RunDeepAuditPRScan:                       true,
		PostDeepAuditInlineCommentsMinSeverity:   "critical",
	}

	state := mapPRChecksAPIToModel(api)
	if state.ID.ValueString() != "1" {
		t.Fatalf("id = %q, want 1", state.ID.ValueString())
	}
	if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %q, want medium", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
	}
	if state.PostDeepAuditInlineCommentsMinSeverity.ValueString() != "critical" {
		t.Fatalf("post_deep_audit_inline_comments_min_severity = %q, want critical", state.PostDeepAuditInlineCommentsMinSeverity.ValueString())
	}
	if !state.RunDeepAuditPRScan.ValueBool() {
		t.Fatalf("run_deep_audit_pr_scan = false, want true")
	}
}

func TestMergePRChecksAPIAndPrior_PreservesIgnoredFields(t *testing.T) {
	api := prChecksConfigurationAPI{
		ID:                                     1,
		CodeRepoID:                             123,
		MinimumSeverity:                        "high",
		FailOnDependencyScan:                   true,
		FailOnSastScan:                         true,
		FailOnIacScan:                          true,
		FailOnSecretsScan:                      true,
		FailOnMalwareScan:                      true,
		PostInlineCommentsMinSeverity:          "none",
		MinimumLicenseSeverity:                 "none",
		FailOnCodeQualityScan:                  false,
		EnableCodeQualityScan:                  false,
		RunDeepAuditPRScan:                     false,
		PostDeepAuditInlineCommentsMinSeverity: "low",
	}
	prior := prChecksConfigurationModel{
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("medium"),
		PostDeepAuditInlineCommentsMinSeverity:   types.StringValue("high"),
	}

	state := mergePRChecksAPIAndPrior(api, &prior)
	if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %q, want medium from prior", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
	}
	if state.PostDeepAuditInlineCommentsMinSeverity.ValueString() != "high" {
		t.Fatalf("post_deep_audit_inline_comments_min_severity = %q, want high from prior", state.PostDeepAuditInlineCommentsMinSeverity.ValueString())
	}
}

func TestMergePRChecksAPIAndPrior_UsesAPIWhenEnabled(t *testing.T) {
	severity := "critical"
	api := prChecksConfigurationAPI{
		ID:                                       1,
		CodeRepoID:                               123,
		MinimumSeverity:                          "high",
		FailOnDependencyScan:                     true,
		FailOnSastScan:                           true,
		FailOnIacScan:                            true,
		FailOnSecretsScan:                        true,
		FailOnMalwareScan:                        true,
		PostInlineCommentsMinSeverity:            "none",
		MinimumLicenseSeverity:                   "none",
		FailOnCodeQualityScan:                    true,
		EnableCodeQualityScan:                    true,
		PostCodeQualityInlineCommentsMinSeverity: &severity,
		RunDeepAuditPRScan:                       true,
		PostDeepAuditInlineCommentsMinSeverity:   "medium",
	}
	prior := prChecksConfigurationModel{
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("low"),
		PostDeepAuditInlineCommentsMinSeverity:   types.StringValue("high"),
	}

	state := mergePRChecksAPIAndPrior(api, &prior)
	if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "critical" {
		t.Fatalf("post_code_quality_inline_comments_min_severity = %q, want critical from API", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
	}
	if state.PostDeepAuditInlineCommentsMinSeverity.ValueString() != "medium" {
		t.Fatalf("post_deep_audit_inline_comments_min_severity = %q, want medium from API", state.PostDeepAuditInlineCommentsMinSeverity.ValueString())
	}
}
