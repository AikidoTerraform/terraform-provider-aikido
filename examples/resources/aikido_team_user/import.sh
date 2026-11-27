# Memberships are imported as team_id:user_id. Only teams created in Aikido can
# be managed this way; memberships of teams synced from a Git provider are
# rejected.
terraform import aikido_team_user.payments_alice 123:456
