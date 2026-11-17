# Scan several tags of one registry image. The source container keeps its own
# tag filter, and each extra tag gets a clone.
resource "aikido_container" "api" {
  id         = "12345"
  active     = true
  tag_filter = "prod-*"
}

resource "aikido_container_clone" "api" {
  for_each = toset(["staging-*", "semver-production"])

  source_container_id = 12345
  tag_filter          = each.value
}

# Clones do not inherit labels, sensitivity or connectivity. Configure them with
# aikido_container, leaving tag_filter out so the clone keeps its own filter.
resource "aikido_container" "api_clones" {
  for_each = aikido_container_clone.api

  id          = each.value.id
  active      = true
  sensitivity = "sensitive"
  labels      = ["api"]
}

# A clone already made in the Aikido UI is managed with aikido_container. It
# shares the source container's name and registry, and its tag filter tells it
# apart from the other clones.
data "aikido_containers" "api_release" {
  name        = "pied-piper/api"
  registry_id = 67
  tag_filter  = "release-*"
}

resource "aikido_container" "api_release" {
  id     = one(data.aikido_containers.api_release.containers).id
  active = true
}
