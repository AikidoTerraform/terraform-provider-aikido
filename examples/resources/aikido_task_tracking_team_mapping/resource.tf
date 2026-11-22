# Maps Aikido teams to task-tracker projects (Linear, Jira, ...).
# This is team mapping, not code-repository mapping.
# Define one resource per task-tracker integration.
# Destroy only removes this resource from Terraform state; remote mappings stay as configured.
resource "aikido_task_tracking_team_mapping" "linear" {
  integration_id = 2

  project_teams_map = {
    "10000" = [1, 2, 3]
    "10001" = [4, 6]
  }
}
