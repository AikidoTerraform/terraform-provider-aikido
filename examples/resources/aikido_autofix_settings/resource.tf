# Manages the workspace-wide Autofix settings (automatic AutoFix PR creation).
# There is exactly one settings object per workspace; destroying this resource
# disables automatic PR creation.
resource "aikido_autofix_settings" "this" {
  enabled = true

  upgrade_type                 = "critical_and_high_only"
  dependency_repos_scope       = "all"
  dependency_repo_ids          = []
  use_aikido_library_for_major = true

  pentest_autofix_type = "critical_and_high_only"

  sast_autofix_type = "critical_and_high_only"
  sast_repos_scope  = "selected"
  sast_repo_ids     = [123, 456]
}
