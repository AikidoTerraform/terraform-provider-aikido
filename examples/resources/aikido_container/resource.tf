# Activate and configure an existing Aikido container.
resource "aikido_container" "example" {
  id     = "12345"
  active = true

  # Supports * wildcards and the special value semver-production.
  tag_filter = "prod-*"

  sensitivity  = "sensitive"
  connectivity = "connected"

  labels = ["production"]

  # Written only when set. Removing it leaves the existing link in place.
  linked_code_repo_id = 67
}

# Omitting tag_filter leaves the container's filter alone.
resource "aikido_container" "development" {
  id     = "12346"
  active = true
}

# The empty string scans the newest image.
resource "aikido_container" "staging" {
  id         = "12347"
  active     = true
  tag_filter = ""
}
