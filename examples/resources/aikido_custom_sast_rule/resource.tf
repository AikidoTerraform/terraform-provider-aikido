# The rule id inside semgrep_rule is replaced by Aikido with CUSTOM-RULE-<id>,
# so any id works. Trailing newlines are removed before sending, so a heredoc
# can be used as-is.
resource "aikido_custom_sast_rule" "no_eval" {
  issue_title = "Avoid eval()"
  tldr        = "eval() runs arbitrary code, so attacker-controlled input leads to code execution."
  how_to_fix  = "Parse the input with JSON.parse() or a dedicated parser instead of evaluating it."
  language    = "JS"
  priority    = 80

  semgrep_rule = <<-EOT
    rules:
      - id: no-eval
        message: Avoid eval()
        languages: [javascript, typescript]
        severity: ERROR
        pattern: eval(...)
  EOT
}

# Keeping rules as files makes them easy to test with the semgrep CLI.
resource "aikido_custom_sast_rule" "no_shell_true" {
  issue_title  = "subprocess called with shell=True"
  tldr         = "shell=True passes the command through a shell, allowing command injection."
  how_to_fix   = "Pass the command as a list of arguments and drop shell=True."
  language     = "PY"
  priority     = 60
  semgrep_rule = file("${path.module}/rules/no-shell-true.yaml")
}
