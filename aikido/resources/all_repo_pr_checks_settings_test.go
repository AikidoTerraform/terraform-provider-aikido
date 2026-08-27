package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/repositories"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestConstructAllPRChecksSettingsBody(t *testing.T) {
	base := allPrChecksSettingsModel{
		ExcludedRepos:                            []int64{1234},
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(false),
		FailOnSastScan:                           types.BoolValue(false),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(false),
		FailOnMalwareScan:                        types.BoolValue(false),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
	}

	tests := []struct {
		name   string
		mutate func(*allPrChecksSettingsModel)
		check  func(t *testing.T, body map[string]any)
	}{
		{
			name: "omits code_repo_id and sends excluded_repos",
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if _, ok := body["code_repo_id"]; ok {
					t.Error("did not expect code_repo_id in body")
				}
				if !reflect.DeepEqual(body["excluded_repos"], []int64{1234}) {
					t.Errorf("excluded_repos = %#v", body["excluded_repos"])
				}
			},
		},
		{
			name: "sends empty excluded_repos when omitted",
			mutate: func(m *allPrChecksSettingsModel) {
				m.ExcludedRepos = nil
			},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if !reflect.DeepEqual(body["excluded_repos"], []int64{}) {
					t.Errorf("excluded_repos = %#v, want empty slice", body["excluded_repos"])
				}
			},
		},
		{
			name: "includes optional fields when set",
			mutate: func(m *allPrChecksSettingsModel) {
				m.FailOnIacScan = types.BoolValue(true)
				m.PostInlineCommentsMinSeverity = types.StringValue("low")
				m.FailOnCodeQualityScan = types.BoolValue(true)
				m.EnableCodeQualityScan = types.BoolValue(true)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringValue("low")
				m.RunDeepAuditPRScan = types.BoolValue(true)
				m.PostDeepAuditInlineCommentsMinSeverity = types.StringValue("medium")
			},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["post_inline_comments_min_severity"] != "low" {
					t.Errorf("post_inline = %#v", body["post_inline_comments_min_severity"])
				}
				if body["post_code_quality_inline_comments_min_severity"] != "low" {
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
			mutate: func(m *allPrChecksSettingsModel) {
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
			name: "deep audit disabled omits inline severity",
			mutate: func(m *allPrChecksSettingsModel) {
				m.RunDeepAuditPRScan = types.BoolValue(false)
				m.PostDeepAuditInlineCommentsMinSeverity = types.StringValue("medium")
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
			if tc.mutate != nil {
				tc.mutate(&model)
			}
			tc.check(t, constructAllPRChecksSettingsBody(model))
		})
	}
}

func validateAllPRChecksSettings(settings allPrChecksSettingsModel) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(validateAllPRChecksSettingsCodeQuality(settings)...)
	diags.Append(validateAllPRChecksSettingsDeepAudit(settings)...)
	return diags
}

func TestValidateAllPRChecksSettings(t *testing.T) {
	validBase := allPrChecksSettingsModel{
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(true),
		FailOnSastScan:                           types.BoolValue(true),
		FailOnIacScan:                            types.BoolValue(true),
		FailOnSecretsScan:                        types.BoolValue(true),
		FailOnMalwareScan:                        types.BoolValue(true),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(true),
		EnableCodeQualityScan:                    types.BoolValue(true),
		PostCodeQualityInlineCommentsMinSeverity: types.StringValue("low"),
		RunDeepAuditPRScan:                       types.BoolValue(false),
	}

	tests := []struct {
		name      string
		mutate    func(*allPrChecksSettingsModel)
		wantError bool
	}{
		{name: "valid settings"},
		{
			name: "code quality enabled rejects omitted inline severity",
			mutate: func(m *allPrChecksSettingsModel) {
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "code quality disabled rejects fail_on_code_quality_scan",
			mutate: func(m *allPrChecksSettingsModel) {
				m.EnableCodeQualityScan = types.BoolValue(false)
				m.FailOnCodeQualityScan = types.BoolValue(true)
				m.PostCodeQualityInlineCommentsMinSeverity = types.StringNull()
			},
			wantError: true,
		},
		{
			name: "deep audit requires a vulnerability scan",
			mutate: func(m *allPrChecksSettingsModel) {
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
			mutate: func(m *allPrChecksSettingsModel) {
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

			diags := validateAllPRChecksSettings(settings)
			if diags.HasError() != tc.wantError {
				t.Errorf("HasError() = %v, want %v (diags: %v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestAllPRChecksSettingsStateFromPlan(t *testing.T) {
	state := allPRChecksSettingsStateFromPlan(allPrChecksSettingsModel{
		ID:                                       types.StringUnknown(),
		ExcludedRepos:                            nil,
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		PostInlineCommentsMinSeverity:            types.StringNull(),
		PostCodeQualityInlineCommentsMinSeverity: types.StringUnknown(),
		RunDeepAuditPRScan:                       types.BoolNull(),
		PostDeepAuditInlineCommentsMinSeverity:   types.StringUnknown(),
	})

	if state.ID.ValueString() != allRepoPRChecksSettingsResourceID {
		t.Errorf("id = %q, want %s", state.ID.ValueString(), allRepoPRChecksSettingsResourceID)
	}
	if state.ExcludedRepos != nil {
		t.Errorf("excluded_repos = %#v, want nil", state.ExcludedRepos)
	}
	if state.PostInlineCommentsMinSeverity.ValueString() != "none" {
		t.Errorf("post_inline = %q, want none", state.PostInlineCommentsMinSeverity.ValueString())
	}
	if state.RunDeepAuditPRScan.IsNull() || state.RunDeepAuditPRScan.IsUnknown() || state.RunDeepAuditPRScan.ValueBool() {
		t.Errorf("run_deep_audit_pr_scan = %#v, want false", state.RunDeepAuditPRScan)
	}
	if !state.PostCodeQualityInlineCommentsMinSeverity.IsNull() {
		t.Errorf("cq inline = %#v, want null", state.PostCodeQualityInlineCommentsMinSeverity)
	}
	if !state.PostDeepAuditInlineCommentsMinSeverity.IsNull() {
		t.Errorf("deep audit inline = %#v, want null", state.PostDeepAuditInlineCommentsMinSeverity)
	}

	kept := allPRChecksSettingsStateFromPlan(allPrChecksSettingsModel{
		MinimumSeverity:                        types.StringValue("critical"),
		FailOnDependencyScan:                   types.BoolValue(true),
		EnableCodeQualityScan:                  types.BoolValue(false),
		FailOnCodeQualityScan:                  types.BoolValue(false),
		MinimumLicenseSeverity:                 types.StringValue("none"),
		RunDeepAuditPRScan:                     types.BoolValue(true),
		PostDeepAuditInlineCommentsMinSeverity: types.StringValue("high"),
	})
	if kept.PostDeepAuditInlineCommentsMinSeverity.ValueString() != "high" {
		t.Errorf("deep audit inline = %q, want high (deprecated noop kept from plan)", kept.PostDeepAuditInlineCommentsMinSeverity.ValueString())
	}
}

func TestSetAllPRChecksSettings_PostsThenUsesPlan(t *testing.T) {
	var posts int
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == allPrChecksSettingsPath:
			posts++
			gotPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			_, _ = io.WriteString(w, `{"success":1}`)

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	plan := allPrChecksSettingsModel{
		ID:                                       types.StringUnknown(),
		ExcludedRepos:                            []int64{1234},
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(false),
		FailOnSastScan:                           types.BoolValue(false),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(false),
		FailOnMalwareScan:                        types.BoolValue(false),
		PostInlineCommentsMinSeverity:            types.StringValue("low"),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
		RunDeepAuditPRScan:                       types.BoolValue(false),
	}

	state, err := (&allPrChecksSettingsResource{client: testClient(srv)}).setAllPRChecksSettings(context.Background(), plan)
	if err != nil {
		t.Fatalf("setAllPRChecksSettings: %v", err)
	}

	if posts != 1 {
		t.Errorf("posts=%d, want 1", posts)
	}
	if gotPath != allPrChecksSettingsPath {
		t.Errorf("path = %q, want %s", gotPath, allPrChecksSettingsPath)
	}
	if _, ok := gotBody["code_repo_id"]; ok {
		t.Error("did not expect code_repo_id in POST body")
	}
	if !reflect.DeepEqual(gotBody["excluded_repos"], []any{float64(1234)}) {
		t.Errorf("excluded_repos = %#v", gotBody["excluded_repos"])
	}
	if state.ID.ValueString() != allRepoPRChecksSettingsResourceID {
		t.Errorf("id = %q, want %s", state.ID.ValueString(), allRepoPRChecksSettingsResourceID)
	}
	if !reflect.DeepEqual(state.ExcludedRepos, []int64{1234}) {
		t.Errorf("state excluded_repos = %#v", state.ExcludedRepos)
	}
	if state.MinimumSeverity.ValueString() != "critical" {
		t.Errorf("minimum_severity = %q, want critical from plan", state.MinimumSeverity.ValueString())
	}
}

func TestSetAllPRChecksSettings_InvalidatesListCache(t *testing.T) {
	var listGets int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == allPrChecksSettingsPath:
			_, _ = io.WriteString(w, `{"success":1}`)

		case r.Method == http.MethodGet && r.URL.Path == prChecksSettingsPath:
			if r.URL.Query().Get("filter_code_repo_id") != "" {
				t.Errorf("unexpected filtered GET %s", r.URL.String())
				w.WriteHeader(http.StatusNotFound)
				return
			}

			listGets++
			_ = json.NewEncoder(w).Encode([]prChecksSettingsAPI{
				{ID: 2, CodeRepoID: 12, MinimumSeverity: "high"},
			})

		case isCodeReposList(r):
			writeReposList(t, w, repositories.Repository{ID: 12, Provider: "github", Active: true})

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	api := testClient(srv)
	if _, err := prChecksSettingsFromCacheForRepo(context.Background(), api, 12); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	if listGets != 1 {
		t.Fatalf("listGets after prime = %d, want 1", listGets)
	}

	plan := allPrChecksSettingsModel{
		ExcludedRepos:                            []int64{},
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(false),
		FailOnSastScan:                           types.BoolValue(false),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(false),
		FailOnMalwareScan:                        types.BoolValue(false),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
	}

	if _, err := (&allPrChecksSettingsResource{client: api}).setAllPRChecksSettings(context.Background(), plan); err != nil {
		t.Fatalf("setAllPRChecksSettings: %v", err)
	}

	if listGets != 1 {
		t.Fatalf("listGets after bulk write = %d, want 1 (write must not refetch the list)", listGets)
	}

	if _, err := prChecksSettingsFromCacheForRepo(context.Background(), api, 12); err != nil {
		t.Fatalf("load after invalidate: %v", err)
	}
	if listGets != 2 {
		t.Errorf("listGets after post-write read = %d, want 2 (cache invalidated)", listGets)
	}
}

func TestGetActualReposToUpdate(t *testing.T) {
	repos := []repositories.Repository{
		{ID: 5, Provider: "gitlab", Active: true},
		{ID: 12, Provider: "github", Active: true, ExternalRepoID: "R_kgDOI5RlKA"},
		{ID: 44, Provider: "github", Active: false},
		{ID: 50, Provider: "github", Active: true, ExternalRepoID: "org_aikidoclone_repo"},
		{ID: 51, Provider: "github", Active: true, ExternalRepoID: "custom_123"},
		{ID: 52, Provider: "github", Active: true, ExternalRepoID: "selfscan_abc"},
		{ID: 1234, Provider: "github", Active: true},
	}

	got := getActualReposToUpdate(repos, []int64{1234})
	if _, ok := got[12]; !ok || len(got) != 1 {
		t.Fatalf("got %#v, want only repo 12", got)
	}
	for _, skipped := range []int64{5, 44, 50, 51, 52, 1234} {
		if _, ok := got[skipped]; ok {
			t.Errorf("repo %d should be skipped", skipped)
		}
	}

	if len(getActualReposToUpdate(repos, []int64{12, 1234})) != 0 {
		t.Fatal("expected empty target when every active GitHub repo is excluded")
	}
}

func TestKeepAllPRChecksSettingsUnlessDrifted(t *testing.T) {
	prior := allPrChecksSettingsModel{
		ID:                                       types.StringValue(allRepoPRChecksSettingsResourceID),
		ExcludedRepos:                            []int64{1234},
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(false),
		FailOnSastScan:                           types.BoolValue(false),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(false),
		FailOnMalwareScan:                        types.BoolValue(false),
		PostInlineCommentsMinSeverity:            types.StringValue("none"),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
		RunDeepAuditPRScan:                       types.BoolValue(false),
	}
	matching := func(codeRepoID int64) prChecksSettingsAPI {
		return prChecksSettingsAPI{
			ID:                            codeRepoID,
			CodeRepoID:                    codeRepoID,
			MinimumSeverity:               "critical",
			MinimumLicenseSeverity:        "none",
			PostInlineCommentsMinSeverity: "none",
		}
	}

	t.Run("keeps prior when every managed repo matches", func(t *testing.T) {
		got := keepAllPRChecksSettingsUnlessDrifted(map[int64]prChecksSettingsAPI{
			12: matching(12),
			99: matching(99),
		}, map[int64]struct{}{12: {}, 99: {}}, prior)
		if got.MinimumSeverity.ValueString() != "critical" {
			t.Fatalf("minimum_severity = %q, want critical from prior", got.MinimumSeverity.ValueString())
		}
		if !reflect.DeepEqual(got.ExcludedRepos, []int64{1234}) {
			t.Errorf("excluded_repos = %#v, want preserved", got.ExcludedRepos)
		}
	})

	t.Run("writes the drifted repo settings not a sentinel", func(t *testing.T) {
		got := keepAllPRChecksSettingsUnlessDrifted(map[int64]prChecksSettingsAPI{
			12: matching(12),
			99: {CodeRepoID: 99, MinimumSeverity: "low", MinimumLicenseSeverity: "none", PostInlineCommentsMinSeverity: "none"},
		}, map[int64]struct{}{12: {}, 99: {}}, prior)
		if got.MinimumSeverity.ValueString() != "low" {
			t.Fatalf("minimum_severity = %q, want low from the drifted repo", got.MinimumSeverity.ValueString())
		}
	})

	t.Run("maps a missing row as unset settings", func(t *testing.T) {
		got := keepAllPRChecksSettingsUnlessDrifted(map[int64]prChecksSettingsAPI{
			12: matching(12),
		}, map[int64]struct{}{12: {}, 99: {}}, prior)
		if got.MinimumSeverity.ValueString() != "" {
			t.Fatalf("minimum_severity = %q, want empty for a missing PR-checks row", got.MinimumSeverity.ValueString())
		}
	})

	t.Run("keeps prior when the managed set is empty", func(t *testing.T) {
		got := keepAllPRChecksSettingsUnlessDrifted(map[int64]prChecksSettingsAPI{
			12: matching(12),
			5:  {CodeRepoID: 5, MinimumSeverity: "low"},
		}, map[int64]struct{}{}, prior)
		if !allPRChecksSettingsEqual(got, prior) {
			t.Fatal("expected prior when no repositories are managed")
		}
	})

	t.Run("keeps prior when a non-managed row differs", func(t *testing.T) {
		got := keepAllPRChecksSettingsUnlessDrifted(map[int64]prChecksSettingsAPI{
			12: matching(12),
			5:  {CodeRepoID: 5, MinimumSeverity: "low"},
		}, map[int64]struct{}{12: {}}, prior)
		if !allPRChecksSettingsEqual(got, prior) {
			t.Fatal("expected prior when only a non-managed repository differs")
		}
	})
}

func TestKeepAllPRChecksSettingsUnlessDrifted_SkipsInactiveNonGitHubAndSynthetic(t *testing.T) {
	repos := []repositories.Repository{
		{ID: 5, Provider: "gitlab", Active: true},
		{ID: 12, Provider: "github", Active: true, ExternalRepoID: "R_kgDOI5RlKA"},
		{ID: 44, Provider: "github", Active: false},
		{ID: 50, Provider: "github", Active: true, ExternalRepoID: "org_aikidoclone_repo"},
		{ID: 51, Provider: "github", Active: true, ExternalRepoID: "custom_123"},
		{ID: 52, Provider: "github", Active: true, ExternalRepoID: "selfscan_abc"},
	}
	desired := allPrChecksSettingsModel{
		ID:                                       types.StringValue(allRepoPRChecksSettingsResourceID),
		MinimumSeverity:                          types.StringValue("critical"),
		FailOnDependencyScan:                     types.BoolValue(false),
		FailOnSastScan:                           types.BoolValue(false),
		FailOnIacScan:                            types.BoolValue(false),
		FailOnSecretsScan:                        types.BoolValue(false),
		FailOnMalwareScan:                        types.BoolValue(false),
		PostInlineCommentsMinSeverity:            types.StringValue("none"),
		MinimumLicenseSeverity:                   types.StringValue("none"),
		FailOnCodeQualityScan:                    types.BoolValue(false),
		EnableCodeQualityScan:                    types.BoolValue(false),
		PostCodeQualityInlineCommentsMinSeverity: types.StringNull(),
		RunDeepAuditPRScan:                       types.BoolValue(false),
	}
	skippedRows := []prChecksSettingsAPI{
		{ID: 1, CodeRepoID: 5, MinimumSeverity: "low"},
		{ID: 3, CodeRepoID: 44, MinimumSeverity: "high"},
		{ID: 4, CodeRepoID: 50, MinimumSeverity: "low"},
		{ID: 5, CodeRepoID: 51, MinimumSeverity: "low"},
		{ID: 6, CodeRepoID: 52, MinimumSeverity: "low"},
	}

	load := func(t *testing.T, rows []prChecksSettingsAPI) bool {
		t.Helper()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == prChecksSettingsPath:
				_ = json.NewEncoder(w).Encode(rows)

			case isCodeReposList(r):
				writeReposList(t, w, repos...)

			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		ctx := context.Background()
		api := testClient(srv)

		settingsList, err := prChecksSettingsListFromCache(ctx, api)
		if err != nil {
			t.Fatalf("prChecksSettingsList: %v", err)
		}

		listedRepos, err := repositories.All(ctx, api)
		if err != nil {
			t.Fatalf("repositories.All: %v", err)
		}

		got := keepAllPRChecksSettingsUnlessDrifted(settingsList, getActualReposToUpdate(listedRepos, nil), desired)
		return allPRChecksSettingsEqual(got, desired)
	}

	t.Run("drift when the managed GitHub repo has no PR-checks row", func(t *testing.T) {
		if load(t, skippedRows) {
			t.Fatal("expected drift when no PR-checks row belongs to the bulk target set")
		}
	})

	t.Run("match when the managed GitHub repo matches and skipped repos differ", func(t *testing.T) {
		rows := append([]prChecksSettingsAPI{
			{
				ID:                            2,
				CodeRepoID:                    12,
				MinimumSeverity:               "critical",
				MinimumLicenseSeverity:        "none",
				PostInlineCommentsMinSeverity: "none",
			},
		}, skippedRows...)

		if !load(t, rows) {
			t.Fatal("expected match: skipped repos must not hide or create drift")
		}
	})
}
