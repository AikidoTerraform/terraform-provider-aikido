# Examples

Terraform snippets for the Aikido provider. See the [Aikido management API docs](https://apidocs.aikido.dev/) for API details.

These are **fragments**, not a complete root module by themselves. Combine the provider example with a resource example (or copy into a scratch directory) before running Terraform.

## Layout

| Path | Purpose |
|------|---------|
| [`provider/provider.tf`](provider/provider.tf) | `required_providers` + `provider "aikido"` block |
| [`resources/aikido_repository/resource.tf`](resources/aikido_repository/resource.tf) | `aikido_repository` resource |
| [`resources/aikido_autofix_dependency_settings/resource.tf`](resources/aikido_autofix_dependency_settings/resource.tf) | `aikido_autofix_dependency_settings` resource |
| [`resources/aikido_autofix_sast_settings/resource.tf`](resources/aikido_autofix_sast_settings/resource.tf) | `aikido_autofix_sast_settings` resource |
| [`resources/aikido_autofix_pentest_settings/resource.tf`](resources/aikido_autofix_pentest_settings/resource.tf) | `aikido_autofix_pentest_settings` resource |

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

Create `main.tf` by combining the examples (replace the repository `id` with a real one from your workspace):

```hcl
terraform {
  required_providers {
    aikido = {
      source = "aikido/aikido"
    }
  }
}

provider "aikido" {}

resource "aikido_repository" "example" {
  id     = "12345"
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
  repo_ids        = [123, 456]
}

resource "aikido_autofix_pentest_settings" "example" {
  enabled      = true
  autofix_type = "critical_and_high_only"
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

- `aikido_repository` configures an **existing** code repository by ID. It does not create the repo in your SCM.
- `labels` is optional. When set, labels are fully managed from Terraform state. Omitting `labels` leaves Aikido labels untouched; `labels = []` fetches current labels from Aikido and deletes them.
- `aikido_autofix_dependency_settings` manages the single **workspace-wide** dependency Autofix settings object; define it at most once. Set `repos_scope` to `all` or `selected`, and pass matching `repo_ids` (ignored when scope is `all`). When `enabled` is `false`, other fields are ignored. Destroying the resource disables automatic dependency AutoFix PR creation.
- `aikido_autofix_sast_settings` manages the single **workspace-wide** SAST & IaC Autofix settings object; define it at most once. Set `repos_scope` to `all` or `selected`, and pass matching `repo_ids` (ignored when scope is `all`). When `enabled` is `false`, other fields are ignored. Destroying the resource disables automatic SAST AutoFix PR creation.
- `aikido_autofix_pentest_settings` manages the single **workspace-wide** Pentest & AI Code Analysis Autofix settings object; define it at most once. When `enabled` is `false`, other fields are ignored. Destroying the resource disables automatic pentest AutoFix PR creation.
- After changing provider Go code locally, re-run `make install` before `terraform apply`.
- Full local/staging setup: [`DEVELOPMENT.md`](../DEVELOPMENT.md)
