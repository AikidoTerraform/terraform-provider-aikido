# Teams are imported by their Aikido team ID. Only teams created in Aikido can be
# imported; teams synced from a Git provider are rejected.
# repository_ids stays unmanaged until it is added to the configuration.
terraform import aikido_team.security 123
