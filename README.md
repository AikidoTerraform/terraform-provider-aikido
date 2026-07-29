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

Optionally override the API base URL to target a non-default region (e.g. AU, ME, US) or the GovCloud instance. It defaults to `https://app.aikido.dev/api` and can also be set via the `AIKIDO_BASE_URL` environment variable:

```hcl
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret
  base_url      = "https://app.aikidogov.us/api"
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
```

## Documentation

Generated provider documentation lives in the [`docs/`](docs/) directory and, once published, on the [Terraform Registry](https://registry.terraform.io/providers/aikido/aikido/latest/docs). Usage examples live under [`examples/`](examples/).

## Contributing

Contributions are welcome. Please read our [Contributing Guide](CONTRIBUTING.md) — including the AI Use Policy — before opening a pull request.

## Security

To report a security vulnerability, see our [Security Policy](SECURITY.md). Please do not open public issues for security reports.

## Code of Conduct

This project follows a [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold it.

