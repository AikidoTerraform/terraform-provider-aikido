# A team with no repository_ids: Terraform owns the team and its name, and leaves
# the repositories it is responsible for to be managed in Aikido.
resource "aikido_team" "security" {
  name = "Security"
}

# Selecting repositories by label turns the GitHub topics and custom properties
# Aikido imports into team responsibilities, so adding a repository to a product
# in GitHub adds it to the team on the next apply.
data "aikido_repositories" "payments" {
  active = true
  labels = ["product:payments"]
}

resource "aikido_team" "payments" {
  name           = "Payments"
  repository_ids = data.aikido_repositories.payments.ids

  # repository_ids is authoritative, so an empty result would unlink every
  # repository from the team. A typo, a renamed GitHub property, or a label sync
  # that has not run yet would all produce that empty result silently.
  lifecycle {
    precondition {
      condition     = length(data.aikido_repositories.payments.ids) > 0
      error_message = "No active Aikido repositories carry product:payments. Refusing to unlink every repository from the Payments team."
    }
  }
}

# One team per product, from a single map of label selectors.
locals {
  product_labels = {
    payments = "product:payments"
    identity = "product:identity"
    platform = "product:platform"
  }
}

data "aikido_repositories" "active" {
  active = true
}

locals {
  repository_ids_by_product = {
    for product, label in local.product_labels :
    product => [
      for repository in data.aikido_repositories.active.repositories :
      tonumber(repository.id) if contains(repository.labels, label)
    ]
  }
}

resource "aikido_team" "product" {
  for_each = local.repository_ids_by_product

  name           = title(each.key)
  repository_ids = each.value

  lifecycle {
    precondition {
      condition     = length(each.value) > 0
      error_message = "No active Aikido repositories matched the label for this product. Refusing to unlink every repository from the team."
    }
  }
}
