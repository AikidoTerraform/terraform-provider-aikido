# Developer Setup & Testing

Guide for building, unit-testing, and running end-to-end Terraform applies against this provider.

For user-facing docs, see [`README.md`](README.md).

## Prerequisites

| Tool | Version | Notes |
|------|---------|--------|
| [Go](https://go.dev/doc/install) | >= 1.26 | Matches `go.mod` |
| [Terraform](https://developer.hashicorp.com/terraform/downloads) | >= 1.0 | Required for e2e applies |
| [golangci-lint](https://golangci-lint.run/welcome/install/) | latest | Optional; used by `make lint` |

Also need an Aikido API client ID and secret (OAuth2 client credentials) from the Aikido dashboard for any live API / Terraform e2e work.

## 1. Clone and install Go modules

```shell
git clone <this-repo-url>
cd terraform-provider-aikido

# Download and verify module dependencies
go mod download
go mod tidy

# Optional: tools used by `make generate` (tfplugindocs)
cd tools
go mod download
go mod tidy
cd ..
```

`go mod download` fetches modules into the local module cache. `go mod tidy` syncs `go.mod` / `go.sum` with what the code actually imports.

## 2. Build and install the provider binary

```shell
# Compile everything
make build
# or: go build -v ./...

# Install the provider binary into $(go env GOPATH)/bin
# (binary name: terraform-provider-aikido)
make install
# or: go install -v .
```

Confirm:

```shell
ls "$(go env GOPATH)/bin/terraform-provider-aikido"
```

## 3. Unit tests (no Aikido account)

These use mocked HTTP and do **not** call the live API:

```shell
make test
# or: go test -v -cover -timeout=120s -parallel=10 ./...
```

Optional lint:

```shell
make lint
```

## 4. Local Terraform e2e (against the live API)

This is the main way to manually exercise create/read/update/destroy with a real workspace.

### 4.1 Point Terraform at your local binary

Terraform normally downloads providers from the registry. For local development, use a [development override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers).

Create or edit `~/.terraformrc` (Windows: `%APPDATA%\terraform.rc`):

```hcl
provider_installation {
  dev_overrides {
    "AikidoTerraform/aikido" = "/Users/YOU/go/bin"
    # Must be the directory that contains terraform-provider-aikido
    # Use: echo "$(go env GOPATH)/bin"
  }

  direct {}
}
```

Replace the path with the output of `go env GOPATH` + `/bin`. After changing Go code, re-run `make install` so Terraform picks up the new binary.

With `dev_overrides` enabled, `terraform init` will warn that it is skipping registry installation for `AikidoTerraform/aikido` — that is expected.

### 4.2 Credentials

```shell
export AIKIDO_CLIENT_ID="your-client-id"
export AIKIDO_CLIENT_SECRET="your-client-secret"
```

Or set `client_id` / `client_secret` in the provider block (prefer env vars so secrets stay out of `.tf` files).

### 4.3 Minimal working config

Create a scratch directory (do **not** commit real IDs/secrets):

```shell
mkdir -p /tmp/aikido-tf-e2e && cd /tmp/aikido-tf-e2e
```

`main.tf`:

```hcl
terraform {
  required_providers {
    aikido = {
      source = "AikidoTerraform/aikido"
    }
  }
}

provider "aikido" {}

# `id` must be an existing code repository in your workspace.
resource "aikido_repository" "example" {
  id     = "12345"
  active = true

  sensitivity  = "sensitive"
  connectivity = "connected"

  labels = [ "payments", "production" ]
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
  post_deep_audit_inline_comments_min_severity    = "high"
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
  post_deep_audit_inline_comments_min_severity    = "medium"
}
```

You can also start from [`examples/`](examples/) (`provider/` + `resources/aikido_repository/`), but wire credentials via env vars rather than committing them.

### 4.4 Apply / verify / destroy

```shell
# After every provider code change:
cd /path/to/terraform-provider-aikido && make install

cd /tmp/aikido-tf-e2e
terraform init   # may warn about dev_overrides — OK
terraform plan
terraform apply  # confirm; mutates the real repo in Aikido

terraform state show aikido_repository.example
terraform refresh
terraform destroy
```

Checklist for a good e2e pass:

1. `apply` activates/configures the repo as expected in the Aikido UI / API  
2. Change `sensitivity` or `active`, `apply` again — state and remote match  
3. `destroy` (or deactivate) leaves the workspace in a sane state  

**Note:** `aikido_repository` manages an *existing* repository by ID; it does not create a new repo in SCM. Use a disposable test repository ID.

### 4.5 Debug logging

```shell
export TF_LOG=INFO
# or: TF_LOG=DEBUG
terraform apply
```

To attach a debugger (Delve), run the provider with `-debug` and follow Terraform’s provider debug flow (`go run . -debug`).

## 5. Acceptance tests (`TF_ACC`)

Framework acceptance tests (`resource.Test`) are planned but not fully wired yet. When they exist:

```shell
export AIKIDO_CLIENT_ID="..."
export AIKIDO_CLIENT_SECRET="..."
# Optional: AIKIDO_BASE_URL for non-default regions or environments

make testacc
# or: TF_ACC=1 go test -v -cover -timeout 120m ./...
```

These hit a **real** Aikido workspace and can create/update/delete resources. Prefer a disposable workspace and avoid running them against a production workspace unless that is intentional and appropriately controlled.

## 6. Useful Make targets

| Target | What it does |
|--------|----------------|
| `make build` | `go build ./...` |
| `make install` | Build + install provider to `GOPATH/bin` |
| `make test` | Unit tests |
| `make testacc` | Acceptance tests (`TF_ACC=1`) |
| `make lint` | `golangci-lint run` |
| `make fmt` | `gofmt` |
| `make generate` | Regenerate docs via `tools/` + tfplugindocs |


## Quick start (copy-paste)

```shell
# Modules + binary
go mod tidy
make install

# Terraform override (~/.terraformrc) — once
# provider_installation { dev_overrides { "AikidoTerraform/aikido" = "$(go env GOPATH)/bin" } direct {} }

export AIKIDO_CLIENT_ID="..."
export AIKIDO_CLIENT_SECRET="..."

make test

# Then terraform init/plan/apply in a scratch dir with aikido_repository
```
