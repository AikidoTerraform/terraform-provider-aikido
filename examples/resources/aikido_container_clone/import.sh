# Clones are imported as source_container_id:clone_id, including clones made in
# the Aikido UI. The clone must share the source container's name and registry.
terraform import 'aikido_container_clone.api["release-*"]' 12345:12399
