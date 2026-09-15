# Looking a user up by email is the usual way to turn a person into the numeric
# ID the membership resources need. Matching ignores case.
data "aikido_users" "alice" {
  email = "alice@example.com"
}

output "alice_user_id" {
  # one(...) fails the plan if the lookup matched no user, or more than one,
  # rather than silently picking an account.
  value = one(data.aikido_users.alice.ids)
}

# Filters combine with AND. Omitting every filter returns the whole workspace,
# deactivated users included.
data "aikido_users" "active_admins" {
  role   = "admin"
  active = true
}

output "active_admin_emails" {
  value = [for user in data.aikido_users.active_admins.users : user.email]
}

# Selecting by anything the filters do not cover is a Terraform expression over
# the users list: an email domain, a name prefix, a login recency check.
data "aikido_users" "all" {}

output "contractor_user_ids" {
  description = "Users authenticating outside the company identity provider."
  value = [
    for user in data.aikido_users.all.users :
    user.id if user.auth_type != "saml" && user.active
  ]
}

output "never_logged_in" {
  description = "Users who have been invited but have never signed in."
  value = [
    for user in data.aikido_users.all.users :
    user.email if user.last_login_timestamp == 0
  ]
}
