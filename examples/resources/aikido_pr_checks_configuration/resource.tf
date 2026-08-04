# Manages pull request checks configuration for one Aikido code repository.
# Import by code_repo_id.
# Destroy only removes this resource from Terraform state; remote PR checks stay as configured.
resource "aikido_pr_checks_configuration" "example" {
  code_repo_id = 12345

  minimum_severity                  = "high"
  fail_on_dependency_scan           = true
  fail_on_sast_scan                 = true
  fail_on_iac_scan                  = true
  fail_on_secrets_scan              = true
  fail_on_malware_scan              = true
  post_inline_comments_min_severity = "critical"

  minimum_license_severity = "high"

  fail_on_code_quality_scan                      = true
  enable_code_quality_scan                       = true
  post_code_quality_inline_comments_min_severity = "medium"

  run_deep_audit_pr_scan                       = true
  post_deep_audit_inline_comments_min_severity = "high"
}
