package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestConstructPRChecksBody(t *testing.T) {
	base := prChecksSettingsModel{
		CodeRepoID:                               types.Int64Value(12),
		MinimumSeverity:                          types.StringValue("high"),
		FailOnDependencyScan:                     types.BoolValue(true),
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(true),
		FailOnSecretsScan:                        types.BoolValue(true),
		FailOnMalwareScan:                        types.BoolValue(true),
		MinimumLicenseSeverity:                   types.StringValue("high"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
	}

	tests := []struct {
		name   string
		mutate func(*prChecksSettingsModel)
		check  func(t *testing.T, body map[string]any)
	}{
		{
			name: "includes optional fields when set",
			mutate: func(m *prChecksSettingsModel) {
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.PostInlineCommentsMinSeverity = types.StringValue("critical")
				m.FailOnCodeQualityScan = types.BoolValue(true)
				m.EnableCodeQualityScan = types.BoolValue(true)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringValue("medium")
				m.RunDeepAuditPRScan = types.BoolValue(true)
				m.PostDeepAuditInlineCommentsMinSeverity = types.StringValue("high")
			},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["code_repo_id"] != int64(12) {
					t.Errorf("code_repo_id = %#v", body["code_repo_id"])
				}
				if body["post_inline_comments_min_severity"] != "critical" {
					t.Errorf("post_inline = %#v", body["post_inline_comments_min_severity"])
				}
				if body["post_code_quality_inline_comments_min_severity"] != "medium" {
					t.Errorf("cq inline = %#v", body["post_code_quality_inline_comments_min_severity"])
				}
				if body["run_deep_audit_pr_scan"] != true {
					t.Errorf("deep audit = %#v", body["run_deep_audit_pr_scan"])
				}
				if _, ok := body["post_deep_audit_inline_comments_min_severity"]; ok {
					t.Error("deprecated post_deep_audit_inline_comments_min_severity must not be sent")
				}
			},
		},
		{
			name: "omits null optional fields",
			mutate: func(m *prChecksSettingsModel) {
				m.PostInlineCommentsMinSeverity = types.StringNull()
				m.RunDeepAuditPRScan = types.BoolNull()
				m.PostDeepAuditInlineCommentsMinSeverity = types.StringNull()
			},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				for _, key := range []string{
					"post_inline_comments_min_severity",
					"post_code_quality_inline_comments_min_severity",
					"run_deep_audit_pr_scan",
					"post_deep_audit_inline_comments_min_severity",
				} {
					if _, ok := body[key]; ok {
						t.Errorf("did not expect %s in body", key)
					}
				}
			},
		},
		{
			name: "sends none as string for inline and license",
			mutate: func(m *prChecksSettingsModel) {
				m.PostInlineCommentsMinSeverity = types.StringValue("none")
				m.MinimumLicenseSeverity = types.StringValue("none")
			},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["post_inline_comments_min_severity"] != "none" {
					t.Errorf("post_inline = %#v", body["post_inline_comments_min_severity"])
				}
				if body["minimum_license_severity"] != "none" {
					t.Errorf("license = %#v", body["minimum_license_severity"])
				}
				if _, ok := body["post_code_quality_inline_comments_min_severity"]; ok {
					t.Error("did not expect cq inline in body")
				}
			},
		},
		{
			name: "deep audit disabled omits inline severity",
			mutate: func(m *prChecksSettingsModel) {
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolValue(false)
				m.PostDeepAuditInlineCommentsMinSeverity = types.StringValue("high")
			},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["run_deep_audit_pr_scan"] != false {
					t.Errorf("deep audit = %#v", body["run_deep_audit_pr_scan"])
				}
				if _, ok := body["post_deep_audit_inline_comments_min_severity"]; ok {
					t.Error("deprecated post_deep_audit_inline_comments_min_severity must not be sent")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := base
			tc.mutate(&model)
			tc.check(t, constructPRChecksSettingsBody(model))
		})
	}
}

func validatePRChecksSettings(settings prChecksSettingsModel) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(validatePRChecksSettingsCodeQuality(settings)...)
	diags.Append(validatePRChecksSettingsDeepAudit(settings)...)
	return diags
}

func TestValidatePRChecksSettings(t *testing.T) {
	validBase := prChecksSettingsModel{
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
	}

	tests := []struct {
		name      string
		mutate    func(*prChecksSettingsModel)
		wantError bool
	}{
		{name: "valid settings"},
		{
			name: "code quality enabled rejects omitted inline severity",
			mutate: func(m *prChecksSettingsModel) {
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "code quality disabled rejects fail_on_code_quality_scan",
			mutate: func(m *prChecksSettingsModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(true)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "code quality disabled allows set or omitted inline severity",
			mutate: func(m *prChecksSettingsModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(false)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringValue("low")
			},
		},
		{
			name: "deep audit requires a vulnerability scan",
			mutate: func(m *prChecksSettingsModel) {
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
			mutate: func(m *prChecksSettingsModel) {
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
			mutate: func(m *prChecksSettingsModel) {
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
			name: "deep audit unknown or null is skipped",
			mutate: func(m *prChecksSettingsModel) {
				m.FailOnDependencyScan = types.BoolValue(false)
				m.FailOnSastScan = types.BoolValue(false)
				m.FailOnIacScan = types.BoolValue(false)
				m.FailOnSecretsScan = types.BoolValue(false)
				m.FailOnMalwareScan = types.BoolValue(false)
				m.MinimumLicenseSeverity = types.StringValue("none")
				m.RunDeepAuditPRScan = types.BoolUnknown()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := validBase
			if tc.mutate != nil {
				tc.mutate(&settings)
			}

			diags := validatePRChecksSettings(settings)
			if diags.HasError() != tc.wantError {
				t.Errorf("HasError() = %v, want %v (diags: %v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestMapAndMergePRChecksAPI(t *testing.T) {
	t.Run("normalizes empty GET defaults", func(t *testing.T) {
		state := mapPRChecksSettingsAPIToModel(prChecksSettingsAPI{
			ID:                    1,
			CodeRepoID:            123,
			MinimumSeverity:       "high",
			FailOnDependencyScan:  true,
			FailOnSastScan:        true,
			FailOnIacScan:         true,
			FailOnSecretsScan:     true,
			FailOnMalwareScan:     true,
			FailOnCodeQualityScan: false,
			EnableCodeQualityScan: false,
			RunDeepAuditPRScan:    false,
		})

		if state.PostInlineCommentsMinSeverity.ValueString() != "none" {
			t.Errorf("post_inline = %q, want none", state.PostInlineCommentsMinSeverity.ValueString())
		}
		if !state.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
			t.Errorf("cq inline = %#v, want null", state.PostCodeQualityInlineCommentsMinSeverity)
		}
		if state.MinimumLicenseSeverity.ValueString() != "none" {
			t.Errorf("license = %q, want none", state.MinimumLicenseSeverity.ValueString())
		}
	})

	t.Run("maps enabled code quality and deep audit", func(t *testing.T) {
		severity := "medium"
		state := mapPRChecksSettingsAPIToModel(prChecksSettingsAPI{
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
		})

		if state.ID.ValueString() != "1" {
			t.Errorf("id = %q", state.ID.ValueString())
		}
		if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
			t.Errorf("cq inline = %q", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
		}
	})

	t.Run("merge keeps ignored fields when features disabled", func(t *testing.T) {
		prior := prChecksSettingsModel{
			PostCodeQualityInlineCommentsMinSeverity: types.StringValue("medium"),
			PostDeepAuditInlineCommentsMinSeverity:   types.StringValue("high"),
		}
		state := mergePRChecksSettingsAPIAndPrior(prChecksSettingsAPI{
			ID:                            1,
			CodeRepoID:                    123,
			MinimumSeverity:               "high",
			FailOnDependencyScan:          true,
			FailOnSastScan:                true,
			FailOnIacScan:                 true,
			FailOnSecretsScan:             true,
			FailOnMalwareScan:             true,
			PostInlineCommentsMinSeverity: "none",
			MinimumLicenseSeverity:        "none",
			FailOnCodeQualityScan:         false,
			EnableCodeQualityScan:         false,
			RunDeepAuditPRScan:            false,
		}, &prior)

		if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "medium" {
			t.Errorf("cq inline = %q, want prior medium", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
		}
		if state.PostDeepAuditInlineCommentsMinSeverity.ValueString() != "high" {
			t.Errorf("deep audit inline = %q, want prior high", state.PostDeepAuditInlineCommentsMinSeverity.ValueString())
		}
	})

	t.Run("merge uses API when features enabled", func(t *testing.T) {
		severity := "critical"
		prior := prChecksSettingsModel{
			PostCodeQualityInlineCommentsMinSeverity: types.StringValue("low"),
			PostDeepAuditInlineCommentsMinSeverity:   types.StringValue("high"),
		}
		state := mergePRChecksSettingsAPIAndPrior(prChecksSettingsAPI{
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
		}, &prior)

		if state.PostCodeQualityInlineCommentsMinSeverity.ValueString() != "critical" {
			t.Errorf("cq inline = %q, want API critical", state.PostCodeQualityInlineCommentsMinSeverity.ValueString())
		}
		if state.PostDeepAuditInlineCommentsMinSeverity.ValueString() != "high" {
			t.Errorf("deep audit inline = %q, want prior high (deprecated noop)", state.PostDeepAuditInlineCommentsMinSeverity.ValueString())
		}
	})

	t.Run("merge normalizes unknown deprecated deep audit field to null", func(t *testing.T) {
		prior := prChecksSettingsModel{
			PostDeepAuditInlineCommentsMinSeverity: types.StringUnknown(),
		}
		state := mergePRChecksSettingsAPIAndPrior(prChecksSettingsAPI{
			ID:                            1,
			CodeRepoID:                    123,
			MinimumSeverity:               "high",
			FailOnDependencyScan:          true,
			FailOnSastScan:                true,
			FailOnIacScan:                 true,
			FailOnSecretsScan:             true,
			FailOnMalwareScan:             true,
			PostInlineCommentsMinSeverity: "none",
			MinimumLicenseSeverity:        "none",
			FailOnCodeQualityScan:         false,
			EnableCodeQualityScan:         false,
			RunDeepAuditPRScan:            false,
		}, &prior)

		if !state.PostDeepAuditInlineCommentsMinSeverity.IsNull() {
			t.Errorf("deep audit inline = %#v, want null", state.PostDeepAuditInlineCommentsMinSeverity)
		}
	})
}

func TestSetPRChecksSettings_PostsThenFetches(t *testing.T) {
	var posts, gets int
	var gotFilter string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			posts++
			_, _ = io.WriteString(w, `{"success":1}`)

		case http.MethodGet:
			gets++
			gotFilter = r.URL.Query().Get("filter_code_repo_id")
			_ = json.NewEncoder(w).Encode([]prChecksSettingsAPI{{
				ID:              99,
				CodeRepoID:      12,
				MinimumSeverity: "critical",
			}})

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	plan := prChecksSettingsModel{
		ID:                                       types.StringUnknown(),
		CodeRepoID:                               types.Int64Value(12),
		MinimumSeverity:                          types.StringValue("critical"),
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
		RunDeepAuditPRScan:                       types.BoolValue(false),
		// Omitted Optional+Computed: UseStateForUnknown has no prior on first apply.
		PostDeepAuditInlineCommentsMinSeverity: types.StringUnknown(),
	}

	state, err := (&prChecksSettingsResource{client: testClient(srv)}).setPRChecksSettings(context.Background(), plan)
	if err != nil {
		t.Fatalf("setPRChecksSettings: %v", err)
	}

	if posts != 1 || gets != 1 {
		t.Errorf("posts=%d gets=%d, want 1 each", posts, gets)
	}
	if gotFilter != "12" {
		t.Errorf("filter_code_repo_id = %q, want 12", gotFilter)
	}
	if state.ID.ValueString() != "99" {
		t.Errorf("id = %q, want 99", state.ID.ValueString())
	}
	if state.MinimumSeverity.ValueString() != "critical" {
		t.Errorf("minimum_severity = %q", state.MinimumSeverity.ValueString())
	}
	if !state.PostDeepAuditInlineCommentsMinSeverity.IsNull() {
		t.Errorf("post_deep_audit_inline_comments_min_severity = %#v, want null", state.PostDeepAuditInlineCommentsMinSeverity)
	}
}
