# Maps Aikido code repositories to task-tracker projects (Linear, Jira, ...).
# This is repository mapping, not Aikido team mapping.
# Define one resource per task-tracker integration.
# Destroy only removes this resource from Terraform state; remote mappings stay as configured.
resource "aikido_task_tracking_code_repo_mapping" "linear" {
  integration_id = 2

  project_repos_map = {
    "10000" = [1, 2, 3]
    "10001" = [4, 6]
  }
}
