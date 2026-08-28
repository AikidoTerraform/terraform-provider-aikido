# Terraform Provider for Aikido Security

[![License: MPL-2.0](https://img.shields.io/badge/License-MPL_2.0-yellow.svg)](https://opensource.org/licenses/MPL-2.0)

The Aikido Terraform provider allows you to manage resources in [Aikido Security](https://www.aikido.dev/) via the [management API](https://apidocs.aikido.dev/).

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://go.dev/doc/install) >= 1.26 (to build the provider)

## Authentication

The provider authenticates using OAuth2 client credentials. You can obtain a client ID and secret from the [Aikido REST API integration page](https://app.aikido.dev/settings/integrations/api/aikido/rest).

```hcl
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret
}
```

Credentials can also be provided via environment variables:

- `AIKIDO_CLIENT_ID`
- `AIKIDO_CLIENT_SECRET`

Optionally override the API base URL to target a non-default region (e.g. AU, ME, US). It defaults to `https://app.aikido.dev/api` and can also be set via the `AIKIDO_BASE_URL` environment variable:

```hcl
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret
  base_url      = "https://app.us.aikido.dev/api"
}
```

## Usage

```hcl
terraform {
  required_providers {
    aikido = {
      source = "AikidoTerraform/aikido"
    }
  }
}

provider "aikido" {}

resource "aikido_repository" "app" {
  id     = "12345"
  active = true

  sensitivity  = "sensitive"
  connectivity = "connected"

  labels = [ "payments", "production"]
}

resource "aikido_autofix_dependency_settings" "example" {
  enabled                      = true
  severity_filter              = "critical_and_high_only"
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
  enabled         = true
  severity_filter = "critical_and_high_only"
}

resource "aikido_repo_pr_checks_settings" "example" {
  code_repo_id                                    = 12345
  
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
}

resource "aikido_default_pr_checks_settings" "example" {
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
}

resource "aikido_all_repo_pr_checks_settings" "example" {
  excluded_repos                                  = [1234]

  minimum_severity                                = "critical"
  fail_on_dependency_scan                         = false
  fail_on_sast_scan                               = false
  fail_on_iac_scan                                = false
  fail_on_secrets_scan                            = false
  fail_on_malware_scan                            = false
  post_inline_comments_min_severity               = "low"

  minimum_license_severity                        = "none"

  fail_on_code_quality_scan                       = false
  enable_code_quality_scan                        = false
  post_code_quality_inline_comments_min_severity  = "low"

  run_deep_audit_pr_scan                          = false
}
```

## Documentation

Generated provider documentation lives in the [`docs/`](docs/) directory and, once published, on the [Terraform Registry](https://registry.terraform.io/providers/AikidoTerraform/aikido/latest/docs). Usage examples live under [`examples/`](examples/).

## Contributing

Contributions are welcome. Please read our [Contributing Guide](CONTRIBUTING.md) — including the AI Use Policy — before opening a pull request.

## Security

To report a security vulnerability, see our [Security Policy](SECURITY.md). Please do not open public issues for security reports.

## Code of Conduct

This project follows a [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold it.
