terraform {
  required_providers {
    aikido = {
      source = "AikidoTerraform/aikido"
    }
  }
}

# Configure the Aikido Provider.
# Credentials can also be set via AIKIDO_CLIENT_ID and AIKIDO_CLIENT_SECRET.
provider "aikido" {
  client_id     = var.aikido_client_id
  client_secret = var.aikido_client_secret

  # Optional: override the API base URL to target a non-default region (e.g. AU,
  # ME, US). Falls back to AIKIDO_BASE_URL.
  # base_url = "https://app.us.aikido.dev/api"

  # Optional: raise the client-side rate limit for outbound API requests.
  # Defaults to 20 requests/min (the standard API limit).
  # Only set this above 20 if the Aikido team has raised your workspace's API
  # rate limit accordingly, otherwise requests will be throttled (HTTP 429).
  # requests_per_minute = 100
}
