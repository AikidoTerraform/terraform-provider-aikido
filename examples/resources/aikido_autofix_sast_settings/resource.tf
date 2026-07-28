# Manages workspace-wide SAST & IaC Autofix settings.
# There is exactly one SAST settings object per workspace.
# Destroying this resource disables automatic SAST & IaC AutoFix PR creation.
resource "aikido_autofix_sast_settings" "my-workspace" {
  enabled      = true
  autofix_type = "critical_and_high_only"
  repos_scope  = "selected"
  repo_ids     = [123, 456]
}
