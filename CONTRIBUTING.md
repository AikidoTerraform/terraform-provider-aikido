# Contributing

Thank you for your interest in contributing to the Aikido Terraform provider.

## Code of Conduct

This project follows the [HashiCorp Community Guidelines](https://www.hashicorp.com/community-guidelines). Please read the guidelines before participating.

## Reporting Bugs

Use the [bug report issue template](https://github.com/AikidoSec/terraform-provider-aikido/issues/new?template=bug-report.md) and include:

- Terraform and provider versions
- A minimal reproducible configuration
- Expected vs. actual behavior
- Relevant debug output (`TF_LOG=DEBUG`) when possible

## Development Setup

See [`README.dev.md`](README.dev.md) for prerequisites, building the provider, local Terraform e2e testing, and available `make` targets.

Quick checks before opening a pull request:

```shell
make fmt
make lint
make test
make generate   # if you changed schemas or provider metadata
```

CI runs the same steps on every pull request (build, unit tests, lint, and docs verification).

## Pull Requests

1. Fork the repository and create a branch from `main`.
2. Make focused changes with clear commit messages.
3. Add or update unit tests for behavior changes. Prefer mocked HTTP tests in `internal/`; they do not require Aikido credentials.
4. Regenerate documentation when resource or provider schemas change:

   ```shell
   make generate
   ```

5. Update [`CHANGELOG.md`](CHANGELOG.md) for user-visible changes (new resources, bug fixes, breaking changes). Follow the existing format under the unreleased section.
6. Open a pull request describing what changed and why. Link related issues when applicable.

## Acceptance Tests

Acceptance tests (`TF_ACC=1`) call the live Aikido API and require OAuth2 credentials.

To run them locally:

```shell
export AIKIDO_CLIENT_ID="..."
export AIKIDO_CLIENT_SECRET="..."
make testacc
```

Use a disposable Aikido workspace—not production customer data.

## License

By contributing, you agree that your contributions will be licensed under the [Mozilla Public License 2.0](https://opensource.org/licenses/MPL-2.0), the same license as this project.
