# Capabilities left out of the configuration are set to false on apply.
data "aikido_users" "alice" {
  email = "alice@example.com"
}

resource "aikido_user_permissions" "alice" {
  user_id = one(data.aikido_users.alice.ids)
  role    = "team_only"

  can_ignore_issues = true
  can_snooze_issues = true
  can_export_data   = true
}

# admin grants every capability and team_only withholds six of them, so setting
# a capability the role decides is a plan-time error.
resource "aikido_user_permissions" "platform_lead" {
  user_id = one(data.aikido_users.platform_lead.ids)
  role    = "admin"
}

data "aikido_users" "platform_lead" {
  email = "lead@example.com"
}

# Permissions granted in bulk from a single description of who gets what.
locals {
  reviewers = {
    "bob@example.com"   = ["can_ignore_issues", "can_change_issue_severity"]
    "carol@example.com" = ["can_ignore_issues", "can_snooze_issues", "can_export_data"]
  }
}

data "aikido_users" "all" {}

resource "aikido_user_permissions" "reviewers" {
  for_each = {
    for user in data.aikido_users.all.users :
    user.email => user.id if contains(keys(local.reviewers), user.email)
  }

  user_id = each.value
  role    = "default"

  can_ignore_issues         = contains(local.reviewers[each.key], "can_ignore_issues")
  can_snooze_issues         = contains(local.reviewers[each.key], "can_snooze_issues")
  can_change_issue_severity = contains(local.reviewers[each.key], "can_change_issue_severity")
  can_export_data           = contains(local.reviewers[each.key], "can_export_data")
}
