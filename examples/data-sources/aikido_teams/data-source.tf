# Filters combine with AND, and every one of them is optional. imported splits
# the workspace into the teams aikido_team can manage and the read-only ones a
# Git provider owns.
data "aikido_teams" "manual" {
  imported = false
}

output "aikido_managed_team_names" {
  value = [for team in data.aikido_teams.manual.teams : team.name]
}

# ids holds the numeric IDs that team_id attributes expect. one(...) returns
# null for no match and fails for multiple matches.
data "aikido_teams" "payments" {
  name = "Payments"
}

output "payments_team_id" {
  value = one(data.aikido_teams.payments.ids)
}

# Each team reports the repositories it is responsible for, so ownership is a
# set operation in Terraform rather than something to track by hand. The same
# shape answers "which repositories does this team cover", "which teams cover
# this repository", and the inverse below.
data "aikido_teams" "synced" {
  imported = true
}

data "aikido_repositories" "active" {
  active = true
}

locals {
  covered_repository_ids = toset(flatten([
    for team in data.aikido_teams.synced.teams : team.repository_ids
  ]))

  uncovered_repository_ids = [
    for repository in data.aikido_repositories.active.repositories :
    tonumber(repository.id) if !contains(local.covered_repository_ids, tonumber(repository.id))
  ]
}

output "uncovered_repository_count" {
  description = "Active repositories no synced team is responsible for."
  value       = length(local.uncovered_repository_ids)
}
