# A repository is cloned once per tag filter, so a name can match several
# containers in one registry. For AWS the registry name is the account ID.
data "aikido_containers" "compression" {
  name          = "pied-piper/compression"
  registry_name = "111222333444"
  tag_filter    = "prod-*"
}

# ids is numeric, so the match feeds image_id with no conversion.
resource "aikido_team_resource" "compression" {
  team_id  = aikido_team.platform.id
  image_id = one(data.aikido_containers.compression.ids)
}

# An omitted tag_filter leaves the container's filter alone.
resource "aikido_container" "compression" {
  id     = one(data.aikido_containers.compression.containers).id
  active = true
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

# Registry and name do not identify a container on their own, so the trailing
# ... groups every match into a list.
locals {
  container_ids_by_registry_and_name = {
    for container in data.aikido_containers.all.containers :
    "${container.registry_name}/${container.name}" => tonumber(container.id)...
  }
}

# An empty tag_filter selects the containers that scan their newest image, and a
# returned container reports that state the same way.
data "aikido_containers" "newest_image" {
  tag_filter = ""
}

output "containers_scanning_the_newest_image" {
  value = data.aikido_containers.newest_image.ids
}
