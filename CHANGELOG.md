## 1.4.0

FEATURES:

- **New Resource:** `aikido_default_pr_checks_settings` — manages the workspace default pull request checks settings applied to newly activated repositories. 

## 1.3.0

FEATURES:

- **New Resource:** `aikido_all_repo_pr_checks_settings` — applies pull request checks settings to every active GitHub repository, with optional `excluded_repos`. Currently only GitHub is supported.

## 1.2.0

FEATURES:

- **New Data Source:** `aikido_repositories` — lets you look up repositories directly from Aikido instead of hard-coding repository IDs. Supports filtering by name, branch, and active state, and can be used to target individual repositories or build dynamic lists for workspace-wide settings.

## 1.1.1

ENHANCEMENTS:

- Faster `terraform plan` in large workspaces by caching repository and PR checks list data instead of fetching each resource individually.
- Added provider-level `requests_per_minute` configuration so customers with higher workspace API limits can increase the client-side request rate.

NOTES:

- Clarified in the PR checks documentation that Deep Review is currently only available in the EU region.
- Updated the `aikido_repo_pr_checks_settings` resource to accept `always_pass_check` for `minimum_severity`, in line with the API.

## 1.1.0

FEATURES:

- **New Resource:** `aikido_repo_pr_checks_settings` — manages pull request checks settings for one Aikido code repository (severity thresholds, fail-on scan types, code quality, Deep Review, and inline comments).

## 1.0.1

NOTES:

- Initial release of the Aikido Terraform provider.
- Authenticates with OAuth2 client credentials (`client_id` / `client_secret`, or `AIKIDO_CLIENT_ID` / `AIKIDO_CLIENT_SECRET`).

FEATURES:

- **New Resource:** `aikido_repository` — manages an existing Aikido code repository by ID. Apply activates/deactivates and optionally sets `sensitivity` and `connectivity`; destroy deactivates the repo (it is never created or deleted in Aikido/SCM).
- **New Resource:** `aikido_autofix_dependency_settings` — manages workspace-wide dependency (libraries) Autofix settings.
- **New Resource:** `aikido_autofix_sast_settings` — manages workspace-wide SAST & IaC Autofix settings.
- **New Resource:** `aikido_autofix_pentest_settings` — manages workspace-wide Pentest & AI Code Analysis Autofix settings.
