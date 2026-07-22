terraform {
  required_providers {
    aikido = {
      source = "aikido/aikido"
    }
  }
}

# Configure the Aikido Provider.
# Credentials can also be set via AIKIDO_CLIENT_ID and AIKIDO_CLIENT_SECRET.
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret

  # Optional: override the API endpoint (e.g. for staging). Falls back to
  # AIKIDO_BASE_URL, then the public API.
  # base_url = "https://app.aikido.dev/api"
}
