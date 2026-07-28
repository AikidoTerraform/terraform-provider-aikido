# Manages the workspace-wide Autofix settings (automatic AutoFix PR creation).
# There is exactly one settings object per workspace; 
# Destroying this resource disables automatic PR creation. All three nested attributes (dependency, sast, pentest) are required.
resource "aikido_autofix_settings" "test-workspace" {
  dependency = {
    enabled                      = true
    upgrade_type                 = "critical_and_high_only"
    repos_scope                  = "all"
    repo_ids                     = []
    use_aikido_library_for_major = true
  }

  sast = {
    enabled      = true
    autofix_type = "critical_and_high_only"
    repos_scope  = "selected"
    repo_ids     = [123, 456]
  }

  pentest = {
    enabled      = true
    autofix_type = "critical_and_high_only"
  }
}
