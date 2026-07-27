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

Optionally override the API base URL (defaults to `https://app.aikido.dev/api`):

```hcl
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret
}
```

## Usage

```hcl
terraform {
  required_providers {
    aikido = {
      source = "aikido/aikido"
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
```

## Documentation

Generated provider documentation lives in the [`docs/`](docs/) directory or via the [Aikido docs](). Usage examples live under [`examples/`](examples/).

