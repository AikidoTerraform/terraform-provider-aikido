# Permissions are imported by the numeric Aikido user ID. The first read fills in
# the current role and every capability, so plan against it before applying.
terraform import aikido_user_permissions.alice 456
