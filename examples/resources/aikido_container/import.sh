# Containers can be imported by their numeric Aikido container ID.
terraform import aikido_container.example 12345

# Or by registry_name/name for containers discovered from a registry.
# Use the registry_name and name returned by the Aikido API (or aikido_containers).
#
# Examples of registry_name by provider:
#   AWS ECR:       12-digit account ID
#   GCP Artifact:  project ID
#   Azure ACR:     registry hostname (e.g. myregistry.azurecr.io)
#   Docker Hub:    namespace or organization
#   Self-hosted:   registry hostname
terraform import aikido_container.example 111222333444/pied-piper/compression
terraform import aikido_container.example myregistry.azurecr.io/event-bus
terraform import aikido_container.example acme-corp/api-gateway

# Append @tag_filter when several clones share the same registry and name.
terraform import aikido_container.example '111222333444/pied-piper/compression@prod-*'

# A trailing @ selects the container that scans its newest image.
terraform import aikido_container.example '111222333444/pied-piper/compression@'
