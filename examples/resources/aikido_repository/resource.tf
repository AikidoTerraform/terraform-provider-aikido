# Activate and configure an existing Aikido code repository.
resource "aikido_repository" "example" {
  id     = "12345"
  active = true

  sensitivity  = "sensitive"
  connectivity = "connected"

  labels = [
    { name = "payments" },
    { name = "production" },
  ]
}
