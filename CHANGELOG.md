## 1.1.0

FEATURES:

- **New Resource:** `aikido_pr_checks_configuration` — manages pull request checks configuration for one Aikido code repository (severity thresholds, fail-on scan types, code quality, Deep Review, and inline comments).

## 1.0.1

NOTES:

- Initial release of the Aikido Terraform provider.
- Authenticates with OAuth2 client credentials (`client_id` / `client_secret`, or `AIKIDO_CLIENT_ID` / `AIKIDO_CLIENT_SECRET`).

FEATURES:

- **New Resource:** `aikido_repository` — manages an existing Aikido code repository by ID. Apply activates/deactivates and optionally sets `sensitivity` and `connectivity`; destroy deactivates the repo (it is never created or deleted in Aikido/SCM).
- **New Resource:** `aikido_autofix_dependency_settings` — manages workspace-wide dependency (libraries) Autofix settings.
- **New Resource:** `aikido_autofix_sast_settings` — manages workspace-wide SAST & IaC Autofix settings.
- **New Resource:** `aikido_autofix_pentest_settings` — manages workspace-wide Pentest & AI Code Analysis Autofix settings.
