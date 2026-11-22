package resources

import (
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/customrules"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func storedCustomRule(semgrepRule string) customrules.Rule {
	return customrules.Rule{
		ID:          12,
		SemgrepRule: semgrepRule,
		IssueTitle:  "Avoid eval",
		TLDR:        "eval runs arbitrary code",
		HowToFix:    "Parse the input instead",
		Language:    "JS",
		Priority:    50,
	}
}

func TestCustomSastRuleModelFromAPI_SemgrepRule(t *testing.T) {
	stored := "rules:\n  - id: CUSTOM-RULE-12\n    message: avoid eval"

	// Aikido rewrites the rule id and the provider strips trailing newlines, so
	// the configured value never equals what is stored and must not show a diff.
	t.Run("a configured rule Aikido rewrote keeps its configured value", func(t *testing.T) {
		configured := types.StringValue("rules:\n  - id: no-eval\n    message: avoid eval\n")

		model := customSastRuleModelFromAPI(storedCustomRule(stored), configured)

		if model.SemgrepRule != configured {
			t.Errorf("SemgrepRule = %v, want the configured %v", model.SemgrepRule, configured)
		}
	})

	t.Run("a rule changed outside Terraform reads back the stored value", func(t *testing.T) {
		configured := types.StringValue("rules:\n  - id: no-eval\n    message: something else\n")

		model := customSastRuleModelFromAPI(storedCustomRule(stored), configured)

		if model.SemgrepRule != types.StringValue(stored) {
			t.Errorf("SemgrepRule = %v, want the stored %q", model.SemgrepRule, stored)
		}
	})

	t.Run("an imported rule reads back the stored value", func(t *testing.T) {
		model := customSastRuleModelFromAPI(storedCustomRule(stored), types.StringNull())

		if model.SemgrepRule != types.StringValue(stored) {
			t.Errorf("SemgrepRule = %v, want the stored %q", model.SemgrepRule, stored)
		}
	})
}

func TestCustomSastRuleModelFromAPI_OtherFields(t *testing.T) {
	rule := storedCustomRule("rules:")
	rule.HasError = true

	model := customSastRuleModelFromAPI(rule, types.StringNull())

	if model.ID != types.StringValue("12") {
		t.Errorf("ID = %v, want string \"12\"", model.ID)
	}
	if model.IssueTitle != types.StringValue("Avoid eval") || model.TLDR != types.StringValue("eval runs arbitrary code") ||
		model.HowToFix != types.StringValue("Parse the input instead") || model.Language != types.StringValue("JS") {
		t.Errorf("text fields = %+v", model)
	}
	if model.Priority != types.Int64Value(50) {
		t.Errorf("Priority = %v", model.Priority)
	}
	if model.HasError != types.BoolValue(true) {
		t.Errorf("HasError = %v", model.HasError)
	}
}

// Terraform rejects a write whose state differs from the plan, so the saved
// model must carry the planned values untouched, trailing newline included.
func TestSavedCustomSastRuleModel(t *testing.T) {
	planned := customSastRuleModel{
		ID:          types.StringUnknown(),
		SemgrepRule: types.StringValue("rules:\n  - id: no-eval\n"),
		IssueTitle:  types.StringValue("Avoid eval"),
		Priority:    types.Int64Value(50),
		HasError:    types.BoolUnknown(),
	}

	saved := savedCustomSastRuleModel(12, planned)

	if saved.ID != types.StringValue("12") {
		t.Errorf("ID = %v, want \"12\"", saved.ID)
	}
	if saved.HasError != types.BoolValue(false) {
		t.Errorf("HasError = %v, want false", saved.HasError)
	}
	if saved.SemgrepRule != planned.SemgrepRule || saved.IssueTitle != planned.IssueTitle || saved.Priority != planned.Priority {
		t.Errorf("saved = %+v, want the planned values", saved)
	}
}

func TestSemgrepRuleHeader(t *testing.T) {
	cases := map[string]bool{
		"rules:\n  - id: no-eval\n": true,
		"rules:\r\n  - id: no-eval": true,
		"rules:":                    true,
		"":                          false,
		"# comment\nrules:\n":       false,
		"\nrules:\n":                false,
		"rules: \n  - id: no-eval":  false,
		"rules:extra\n":             false,
		"  rules:\n":                false,
	}

	for rule, wantValid := range cases {
		if got := semgrepRuleHeader.MatchString(rule); got != wantValid {
			t.Errorf("semgrepRuleHeader.MatchString(%q) = %v, want %v", rule, got, wantValid)
		}
	}
}

func TestCustomSastRuleErrorDiagnostics(t *testing.T) {
	t.Run("a healthy rule has no warning", func(t *testing.T) {
		if diagnostics := customSastRuleErrorDiagnostics(storedCustomRule("rules:")); len(diagnostics) != 0 {
			t.Errorf("got %v, want none", diagnostics)
		}
	})

	t.Run("a failing rule warns and names the rule", func(t *testing.T) {
		rule := storedCustomRule("rules:")
		rule.HasError = true

		diagnostics := customSastRuleErrorDiagnostics(rule)
		if diagnostics.HasError() || diagnostics.WarningsCount() != 1 {
			t.Fatalf("got %v, want a single warning", diagnostics)
		}
		detail := diagnostics.Warnings()[0].Detail()
		if !strings.Contains(detail, "12") || !strings.Contains(detail, "Avoid eval") {
			t.Errorf("detail %q should name the rule", detail)
		}
	})
}
