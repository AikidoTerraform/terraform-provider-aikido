# Custom rules are imported by their Aikido custom rule ID, listed by the
# aikido_custom_sast_rules data source. The imported semgrep_rule carries
# Aikido's CUSTOM-RULE-<id> rule id, so a configuration using its own rule id
# shows one update on the first plan after import.
terraform import aikido_custom_sast_rule.no_eval 123
