terraform {
  required_providers {
    aikido = {
      source = "AikidoSec/aikido"
    }
  }
}

# Configure the Aikido Provider.
# Credentials can also be set via AIKIDO_CLIENT_ID and AIKIDO_CLIENT_SECRET.
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret

  # Optional: override the API base URL to target a non-default region (e.g. AU,
  # ME, US). Falls back to
  # AIKIDO_BASE_URL, then the default public API.
  # base_url = "https://app.us.aikido.dev/api"
}
