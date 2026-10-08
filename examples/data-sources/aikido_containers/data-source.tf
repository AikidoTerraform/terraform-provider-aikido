# Container names are not unique. Combine filters to select the intended one.
data "aikido_containers" "compression" {
  name          = "pied-piper/compression"
  registry_name = "111222333444"
  tag_filter    = "prod-*"
}

# Use the matching container as a team resource.
resource "aikido_team_resource" "compression" {
  team_id  = aikido_team.platform.id
  image_id = one(data.aikido_containers.compression.ids)
}

# Use the matching container's ID to manage it.
resource "aikido_container" "compression" {
  id     = one(data.aikido_containers.compression.containers).id
  active = true
}

# Find every container in a registry.
data "aikido_containers" "development" {
  registry_name = "999888777666"
}

# Manage every matching container.
resource "aikido_container" "development" {
  for_each = {
    for container in data.aikido_containers.development.containers :
    container.id => container
  }

  id     = each.key
  active = true
}

# Use Terraform expressions to select containers by naming convention.
data "aikido_containers" "all" {}

output "ci_container_ids" {
  description = "Numeric IDs of containers whose name starts with ci-."
  value = [
    for container in data.aikido_containers.all.containers :
    tonumber(container.id) if startswith(container.name, "ci-")
  ]
}

# An empty tag_filter selects containers that scan their newest image.
data "aikido_containers" "newest_image" {
  tag_filter = ""
}

output "containers_scanning_the_newest_image" {
  value = data.aikido_containers.newest_image.ids
}
