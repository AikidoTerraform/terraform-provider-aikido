# Import the workspace default mapping (no integration_id).
terraform import aikido_task_tracking_code_repo_mapping.linear task_tracking_code_repo_mapping

# Import a mapping for a specific task-tracker integration.
terraform import aikido_task_tracking_code_repo_mapping.linear 2
