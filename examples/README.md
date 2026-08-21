# Examples

Terraform snippets for the Aikido provider. See the [Aikido management API docs](https://apidocs.aikido.dev/) for API details.

These are **fragments**, not a complete root module by themselves. Combine the provider example with a resource example (or copy into a scratch directory) before running Terraform.

## Layout

| Path | Purpose |
|------|---------|
| [`provider/provider.tf`](provider/provider.tf) | `required_providers` + `provider "aikido"` block |
| [`data-sources/aikido_repositories/data-source.tf`](data-sources/aikido_repositories/data-source.tf) | `aikido_repositories` data source: look up repositories by name instead of by numeric ID |
| [`resources/aikido_repository/resource.tf`](resources/aikido_repository/resource.tf) | `aikido_repository` resource |
| [`resources/aikido_autofix_dependency_settings/resource.tf`](resources/aikido_autofix_dependency_settings/resource.tf) | `aikido_autofix_dependency_settings` resource |
| [`resources/aikido_autofix_sast_settings/resource.tf`](resources/aikido_autofix_sast_settings/resource.tf) | `aikido_autofix_sast_settings` resource |
| [`resources/aikido_autofix_pentest_settings/resource.tf`](resources/aikido_autofix_pentest_settings/resource.tf) | `aikido_autofix_pentest_settings` resource |
| [`resources/aikido_repo_pr_checks_settings/resource.tf`](resources/aikido_repo_pr_checks_settings/resource.tf) | `aikido_repo_pr_checks_settings` resource |
| [`resources/aikido_all_repo_pr_checks_settings/resource.tf`](resources/aikido_all_repo_pr_checks_settings/resource.tf) | `aikido_all_repo_pr_checks_settings` resource |

## Prerequisites

- Terraform >= 1.0
- Aikido API credentials (`AIKIDO_CLIENT_ID` / `AIKIDO_CLIENT_SECRET`)
- For a **local** provider build: install the binary and set a `dev_overrides` entry — see [`DEVELOPMENT.md`](../DEVELOPMENT.md)

## Quick start

```shell
export AIKIDO_CLIENT_ID="your-client-id"
export AIKIDO_CLIENT_SECRET="your-client-secret"

mkdir -p /tmp/aikido-tf-example && cd /tmp/aikido-tf-example
```

Create `main.tf` by combining the examples (replace the repository names with real ones from your workspace):

```hcl
terraform {
  required_providers {
    aikido = {
      source = "AikidoTerraform/aikido"
    }
  }
}

provider "aikido" {}

# Look up repositories by name, so no numeric Aikido IDs appear in config.
data "aikido_repositories" "payments" {
  name = "payments"
}

# Every repository, for selecting groups with Terraform expressions.
data "aikido_repositories" "all" {}

resource "aikido_repository" "example" {
  id     = one(data.aikido_repositories.payments.repositories).id
  active = true

  sensitivity  = "sensitive"
  connectivity = "connected"

  labels = [ "payments", "production" ]
}

resource "aikido_autofix_dependency_settings" "example" {
  enabled                      = true
  severity_filter                 = "critical_and_high_only"
  repos_scope                  = "all"
  repo_ids                     = []
  use_aikido_library_for_major = true
}

resource "aikido_autofix_sast_settings" "example" {
  enabled         = true
  severity_filter = "critical_and_high_only"
  repos_scope     = "selected"

  # Select by naming convention rather than listing IDs by hand.
  repo_ids = [
    for repository in data.aikido_repositories.all.repositories :
    tonumber(repository.id) if startswith(repository.name, "team-a-")
  ]
}

resource "aikido_autofix_pentest_settings" "example" {
  enabled         = true
  severity_filter = "critical_and_high_only"
}

resource "aikido_repo_pr_checks_settings" "example" {
  code_repo_id                                    = one(data.aikido_repositories.payments.ids)
  
  minimum_severity                                = "high"
  fail_on_dependency_scan                         = true
  fail_on_sast_scan                               = true
  fail_on_iac_scan                                = true
  fail_on_secrets_scan                            = true
  fail_on_malware_scan                            = true
  post_inline_comments_min_severity               = "critical"

  minimum_license_severity                        = "high"

  fail_on_code_quality_scan                       = true
  enable_code_quality_scan                        = true
  post_code_quality_inline_comments_min_severity  = "medium"

  run_deep_audit_pr_scan                          = true
  post_deep_audit_inline_comments_min_severity    = "high"
}

resource "aikido_all_repo_pr_checks_settings" "example" {
  # Skip repositories that should keep their current PR checks settings.
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
```

Then:

```shell
terraform init
terraform plan
terraform apply
```

Prefer env vars for credentials so secrets are not committed. The provider block in [`provider/provider.tf`](provider/provider.tf) shows the attribute form; omitting `client_id` / `client_secret` and using env vars is fine for local runs.

## Notes

- `aikido_repositories` is a read-only lookup, so config never has to hard-code Aikido's numeric repository IDs. Filter with `name`, `branch` and `active` (combined with AND); omit all three to get every repository, active and inactive.
  - `name` matches **exactly** — it is not a substring or glob match. To select repositories by naming convention, omit `name` and filter the `repositories` list with a Terraform expression such as `startswith(repository.name, "team-a-")`, `endswith(...)` or `can(regex(...))`.
  - Use `ids` (Set of Number) wherever a resource wants numeric IDs: assign it straight to `repo_ids`, or `one(...ids)` to a single `code_repo_id`. Use `repositories[*].id` (String) for `aikido_repository`'s `id`.
  - One data source reads the whole repository list once per Terraform run and shares it with every other data source and `aikido_repository` resource in the same run, so extra lookups cost no extra API calls.
- `aikido_repository` configures an **existing** code repository by ID. It does not create the repo in your SCM.
- `labels` is optional. When set, labels are fully managed from Terraform state. Omitting `labels` leaves Aikido labels untouched; `labels = []` fetches current labels from Aikido and deletes them.
- `aikido_autofix_dependency_settings` manages the single **workspace-wide** dependency Autofix settings object; define it at most once. Set `repos_scope` to `all` or `selected`, and pass matching `repo_ids` (ignored when scope is `all`). When `enabled` is `false`, other fields are ignored. Destroying the resource disables automatic dependency AutoFix PR creation.
- `aikido_autofix_sast_settings` manages the single **workspace-wide** SAST & IaC Autofix settings object; define it at most once. Set `repos_scope` to `all` or `selected`, and pass matching `repo_ids` (ignored when scope is `all`). When `enabled` is `false`, other fields are ignored. Destroying the resource disables automatic SAST AutoFix PR creation.
- `aikido_autofix_pentest_settings` manages the single **workspace-wide** Pentest & AI Code Analysis Autofix settings object; define it at most once. When `enabled` is `false`, other fields are ignored. Destroying the resource disables automatic pentest AutoFix PR creation.
- `aikido_repo_pr_checks_settings` manages PR checks for one code repository (`code_repo_id`). When `enable_code_quality_scan` is `true`, `post_code_quality_inline_comments_min_severity` is required; when it is `false`, `fail_on_code_quality_scan` must be `false`. Deep Review (`run_deep_audit_pr_scan`) needs at least one vulnerability scan type enabled. Import by `code_repo_id`.
- `aikido_all_repo_pr_checks_settings` applies the same PR checks settings to every active GitHub repository; define it at most once. Currently only GitHub is supported. Use `excluded_repos` to skip repositories that should keep their current settings. The same code-quality and Deep Review rules apply as for the per-repo resource. Destroy only removes the resource from Terraform state. Import by the singleton id `all_repo_pr_checks_settings`.
- After changing provider Go code locally, re-run `make install` before `terraform apply`.
- Full local/staging setup: [`DEVELOPMENT.md`](../DEVELOPMENT.md)
