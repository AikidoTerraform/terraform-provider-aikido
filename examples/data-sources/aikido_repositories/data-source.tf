data "aikido_repositories" "payments" {
  name   = "payments"
  branch = "main"
}

resource "aikido_repository" "payments" {
  id     = one(data.aikido_repositories.payments.repositories).id
  active = true
}

# ids is numeric, so the name and branch match feeds code_repo_id with no conversion.
resource "aikido_repo_pr_checks_settings" "payments" {
  code_repo_id = one(data.aikido_repositories.payments.ids)

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

# ids also feeds excluded_repos on the workspace-wide PR checks bulk apply.
resource "aikido_all_repo_pr_checks_settings" "workspace" {
  excluded_repos = data.aikido_repositories.payments.ids

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

# Filters combine with AND. Omitting every filter returns all repositories,
# active and inactive.
data "aikido_repositories" "active" {
  active = true
}

# ids matches the type of repo_ids, so scoping workspace-wide Autofix settings to
# every active repository needs no hand-maintained list of integers.
resource "aikido_autofix_sast_settings" "example" {
  enabled         = true
  severity_filter = "critical_and_high_only"
  repos_scope     = "selected"
  repo_ids        = data.aikido_repositories.active.ids
}

# The name filter is an exact match. Selecting repositories by naming convention
# is done with a Terraform expression over the repositories list: startswith for a
# prefix, endswith for a suffix, or can(regex(...)) for anything more involved.
data "aikido_repositories" "all" {}

resource "aikido_autofix_dependency_settings" "team_a" {
  enabled                      = true
  severity_filter              = "critical_and_high_only"
  repos_scope                  = "selected"
  use_aikido_library_for_major = true

  repo_ids = [
    for repository in data.aikido_repositories.all.repositories :
    tonumber(repository.id) if startswith(repository.name, "team-a-")
  ]
}

# A lookup map keyed by name replaces an out-of-band name-to-ID mapping.
locals {
  repository_ids_by_name = {
    for repository in data.aikido_repositories.all.repositories :
    repository.name => tonumber(repository.id)
  }
}

output "payments_repository_id" {
  value = local.repository_ids_by_name["payments"]
}

output "service_repository_ids" {
  description = "Numeric IDs of repositories whose name ends in -service or -api."
  value = [
    for repository in data.aikido_repositories.all.repositories :
    tonumber(repository.id) if can(regex("-(service|api)$", repository.name))
  ]
}

output "never_scanned_repositories" {
  description = "Repositories Aikido has not scanned yet (last_scanned_at is -1)."
  value = [
    for repository in data.aikido_repositories.all.repositories :
    repository.name if repository.last_scanned_at == -1
  ]
}
