# Links are imported as team_id:kind:resource_id. Kind is one of repo, cloud,
# image, domain, zen_app. Only teams created in Aikido can be managed this way;
# resources of teams synced from a Git provider are rejected.
terraform import aikido_team_resource.payments_cloud 123:cloud:12
terraform import aikido_team_resource.payments_frontend 123:repo:4
