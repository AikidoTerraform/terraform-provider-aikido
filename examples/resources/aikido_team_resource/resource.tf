# Link a cloud, container image, domain or Zen app one at a time. Unlike
# aikido_team.repository_ids, this does not replace the team's other
# responsibilities.
resource "aikido_team" "payments" {
  name = "Payments"
}

resource "aikido_team_resource" "payments_cloud" {
  team_id  = aikido_team.payments.id
  cloud_id = 12
}

resource "aikido_team_resource" "payments_image" {
  team_id  = aikido_team.payments.id
  image_id = 8
}

# A code repository can be limited to certain paths. Do not also list this
# repository in aikido_team.repository_ids: that attribute replaces the team's
# whole repository set and would unlink this resource.
resource "aikido_team_resource" "payments_frontend" {
  team_id = aikido_team.payments.id
  repo_id = 4

  repo_path_limitation = {
    limitation_type = "include"
    paths           = ["/client/", "/tests/client/"]
  }
}
