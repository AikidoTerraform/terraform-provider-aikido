# Add two tag filters to existing container 12345.
# The source container keeps its own tag filter.
resource "aikido_container_clone" "api" {
  for_each = toset(["staging-*", "semver-production"])

  source_container_id = 12345
  tag_filter          = each.value
}
