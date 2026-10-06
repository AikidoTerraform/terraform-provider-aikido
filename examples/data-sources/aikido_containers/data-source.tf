# The same repository name can exist in several registries. For AWS the registry
# name is the account ID, so it selects one of two identically named repositories.
data "aikido_containers" "compression" {
  name          = "pied-piper/compression"
  registry_name = "111222333444"
}

# ids is numeric, so the match feeds image_id with no conversion.
resource "aikido_team_resource" "compression" {
  team_id  = aikido_team.platform.id
  image_id = one(data.aikido_containers.compression.ids)
}

resource "aikido_container" "compression" {
  id         = one(data.aikido_containers.compression.containers).id
  active     = true
  tag_filter = "prod-*"
}

# Filters combine with AND. Omitting every filter returns all containers, active
# and inactive.
data "aikido_containers" "development" {
  registry_name = "999888777666"
}

# The nested id is a string, so it feeds aikido_container.id directly while ids
# stays numeric for image_id.
resource "aikido_container" "development" {
  for_each = {
    for container in data.aikido_containers.development.containers :
    container.id => container
  }

  id     = each.key
  active = true
  # tag_filter omitted: scan the newest image.
}

# The name filter is an exact match. Selecting containers by naming convention is
# done with a Terraform expression over the containers list.
data "aikido_containers" "all" {}

output "ci_container_ids" {
  description = "Numeric IDs of containers whose name starts with ci-."
  value = [
    for container in data.aikido_containers.all.containers :
    tonumber(container.id) if startswith(container.name, "ci-")
  ]
}

# A lookup map keyed by registry and name replaces an out-of-band mapping.
locals {
  container_ids_by_registry_and_name = {
    for container in data.aikido_containers.all.containers :
    "${container.registry_name}/${container.name}" => tonumber(container.id)
  }
}

output "containers_scanning_the_newest_image" {
  description = "Containers with no tag filter, which scan the newest image."
  value = [
    for container in data.aikido_containers.all.containers :
    container.name if container.tag_filter == null
  ]
}
