# Applies pull request checks settings to every active GitHub repository.
# Currently only GitHub is supported.
# There is exactly one all-repos PR checks settings object per workspace.
# Use excluded_repos to skip repositories that should keep their current settings.
# Destroy only removes this resource from Terraform state; remote PR checks stay as configured.
resource "aikido_all_repo_pr_checks_settings" "example" {
  excluded_repos = [1234]

  minimum_severity                  = "critical"
  fail_on_dependency_scan           = false
  fail_on_sast_scan                 = false
  fail_on_iac_scan                  = false
  fail_on_secrets_scan              = false
  fail_on_malware_scan              = false
  post_inline_comments_min_severity = "low"

  minimum_license_severity = "none"

  fail_on_code_quality_scan                      = false
  enable_code_quality_scan                       = false
  post_code_quality_inline_comments_min_severity = "low"

  run_deep_audit_pr_scan                       = false
  post_deep_audit_inline_comments_min_severity = "medium"
}
