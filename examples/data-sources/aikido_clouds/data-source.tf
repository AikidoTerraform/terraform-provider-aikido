# All clouds in the workspace.
data "aikido_clouds" "all" {}

# AWS clouds only.
data "aikido_clouds" "aws" {
  cloud_provider = "aws"
}

# AWS organization management accounts (includes organization sync settings).
data "aikido_clouds" "org_management" {
  cloud_provider        = "aws"
  is_management_account = true
}

output "org_synced_unit_ids" {
  description = "OU/root IDs synced for each AWS organization management account."
  value = [
    for cloud in data.aikido_clouds.org_management.clouds :
    cloud.aws.organization.synced_unit_ids
    if cloud.aws != null && cloud.aws.organization != null
  ]
}

# Feed matching cloud IDs into team links.
data "aikido_clouds" "azure_management" {
  cloud_provider        = "azure"
  is_management_account = true
}

output "azure_management_group_ids" {
  value = [
    for cloud in data.aikido_clouds.azure_management.clouds :
    cloud.azure.organization.management_group_ids
  ]
}

resource "aikido_team_resource" "clouds" {
  for_each = toset([for id in data.aikido_clouds.aws.ids : tostring(id)])

  team_id  = aikido_team.security.id
  cloud_id = tonumber(each.value)
}
