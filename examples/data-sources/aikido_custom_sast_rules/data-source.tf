data "aikido_custom_sast_rules" "all" {}

# IDs to import rules created in the Aikido dashboard into aikido_custom_sast_rule.
output "custom_rule_ids_by_title" {
  value = { for rule in data.aikido_custom_sast_rules.all.rules : rule.issue_title => rule.id }
}

# Rules that failed during a scan find nothing until they are fixed.
output "failing_custom_rules" {
  value = [for rule in data.aikido_custom_sast_rules.all.rules : rule.issue_title if rule.has_error]
}
