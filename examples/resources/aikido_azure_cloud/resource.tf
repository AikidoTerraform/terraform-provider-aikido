variable "azure_client_secret" {
  type      = string
  sensitive = true
}

# Register an application and grant the required Azure permissions first.
resource "aikido_azure_cloud" "subscription" {
  name            = "Azure production"
  application_id  = "00000000-0000-0000-0000-000000000001"
  directory_id    = "00000000-0000-0000-0000-000000000002"
  subscription_id = "00000000-0000-0000-0000-000000000003"
  key_value       = var.azure_client_secret
  environment     = "production"
}

resource "aikido_azure_cloud" "management" {
  name            = "Azure organization"
  application_id  = "00000000-0000-0000-0000-000000000001"
  directory_id    = "00000000-0000-0000-0000-000000000002"
  subscription_id = "00000000-0000-0000-0000-000000000003"
  key_value       = var.azure_client_secret
  environment     = "mixed"

  organization = {
    management_group_ids          = ["production"]
    excluded_management_group_ids = ["sandbox"]
    excluded_subscription_ids     = []
    acr_scan_enabled              = true
    vm_scan_enabled               = true
  }
}

# Use organization = {} to sync the tenant root with scanning flags disabled.
resource "aikido_team_resource" "azure_management" {
  team_id  = aikido_team.security.id
  cloud_id = tonumber(aikido_azure_cloud.management.id)
}
