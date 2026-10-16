# Looks up exactly one repository by name. The name is filtered by the API, so
# the lookup stays one small request however many repositories the workspace has.
data "aikido_repository" "payments" {
  name = "payments"
}

resource "aikido_repository" "payments" {
  id     = data.aikido_repository.payments.id
  active = true
}

# id is a string to match aikido_repository; tonumber feeds the numeric
# code_repo_id and repo_ids attributes of the other resources.
resource "aikido_repo_pr_checks_settings" "payments" {
  code_repo_id = tonumber(data.aikido_repository.payments.id)

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

  run_deep_audit_pr_scan = true
}

# A repository that scans more than one branch is cloned in Aikido: each branch
# is a separate repository with the same name, so aikido_repository errors for
# it. Select one clone with aikido_repositories and its branch filter instead.
data "aikido_repositories" "checkout_develop" {
  name   = "checkout"
  branch = "develop"
}

resource "aikido_repository" "checkout_develop" {
  id     = one(data.aikido_repositories.checkout_develop.repositories).id
  active = true
}
