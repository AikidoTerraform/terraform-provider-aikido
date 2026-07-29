# Manages workspace-wide dependency (libraries) Autofix settings.
# There is exactly one dependency settings object per workspace.
# Destroying this resource disables automatic dependency AutoFix PR creation.
resource "aikido_autofix_dependency_settings" "my-workspace" {
  enabled                      = true
  severity_filter              = "critical_and_high_only"
  repos_scope                  = "selected"
  repo_ids                     = [123, 456]
  use_aikido_library_for_major = true
}
