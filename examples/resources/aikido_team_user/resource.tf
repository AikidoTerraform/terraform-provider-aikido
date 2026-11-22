# Repositories selected by their imported GitHub labels, a team responsible for
# them, and the people on that team: the whole chain in one configuration.
data "aikido_repositories" "payments" {
  active = true
  labels = ["product:payments"]
}

resource "aikido_team" "payments" {
  name           = "Payments"
  repository_ids = data.aikido_repositories.payments.ids

  lifecycle {
    precondition {
      condition     = length(data.aikido_repositories.payments.ids) > 0
      error_message = "No active Aikido repositories carry product:payments. Refusing to unlink every repository from the Payments team."
    }
  }
}

data "aikido_users" "alice" {
  email = "alice@example.com"
}

resource "aikido_team_user" "payments_alice" {
  team_id = aikido_team.payments.id
  user_id = one(data.aikido_users.alice.ids)
}

# A security team that needs visibility over every product, whose members are not
# assigned to those repositories in GitHub. Aikido teams are independent of the
# Git provider, so this membership is not undone by the next GitHub sync.
resource "aikido_team" "security" {
  name           = "Security"
  repository_ids = data.aikido_repositories.active.ids
}

data "aikido_repositories" "active" {
  active = true
}

# team_only users see only their teams' repositories, so this membership is what
# grants access. Default users already see every repository.
data "aikido_users" "security_engineers" {
  role   = "team_only"
  active = true
}

# One membership per person keeps each assignment separately owned, so removing
# someone from the list removes only that membership.
resource "aikido_team_user" "security" {
  for_each = {
    for user in data.aikido_users.security_engineers.users :
    user.email => user.id if endswith(user.email, "@security.example.com")
  }

  team_id = aikido_team.security.id
  user_id = each.value
}
