## Unreleased

FEATURES:

- **New Resource:** `aikido_team_resource` — links one resource (code repository, cloud, container image, domain or Zen app) to a team created in Aikido, including optional repository path limitations, via the public `linkResourceToTeam` / `unlinkResourceFromTeam` APIs.

## 1.5.0

FEATURES:

- **New Resource:** `aikido_task_tracking_team_mapping` — maps Aikido teams to task-tracker projects (Linear, Jira, and others) via the public `mapTeamsToProjects` API.
- **New Resource:** `aikido_team` — manages a team created in Aikido and, optionally, the code repositories it is responsible for. Teams synced from a Git provider belong to that provider and are rejected.
- **New Data Source:** `aikido_teams` — looks up Aikido teams, including those synced from a Git provider, and reports the code repositories each is responsible for.
- **New Data Source:** `aikido_users` — looks up workspace users, filtering by ID, email, full name, role, authentication type or activation state.
- **New Resource:** `aikido_team_user` — manages one user's membership of a team created in Aikido. Memberships of teams synced from a Git provider are rejected.
- **New Resource:** `aikido_user_permissions` — manages the role and permissions of a user who already exists in Aikido. Capability attributes are authoritative; removing the resource leaves the user's permissions unchanged.

ENHANCEMENTS:

- `aikido_repositories`: added a `labels` filter, returning repositories that carry every listed label. Labels imported from GitHub topics and custom properties match the same as labels managed in Aikido.

BUG FIXES:

- `aikido_repository`: activation, sensitivity, connectivity and label writes now drop the cached repository list, so a data source reading later in the same apply sees the change.
- `aikido_repository`: a failure while fetching the repository list no longer removes the resource from Terraform state. State is removed only when a successful response confirms that the repository is absent.

## 1.4.0

FEATURES:

- **New Resource:** `aikido_default_pr_checks_settings` — manages the workspace default pull request checks settings applied to newly activated repositories.

## 1.3.1

NOTES:

- `aikido_repo_pr_checks_settings` and `aikido_all_repo_pr_checks_settings`: `post_deep_audit_inline_comments_min_severity` is deprecated and ignored (no-op). Existing configurations keep working; the argument can be removed.

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
