package datasources

import (
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/customrules"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCustomSastRuleModelFromAPI(t *testing.T) {
	rule := customrules.Rule{
		ID:          12,
		SemgrepRule: "rules:\n  - id: CUSTOM-RULE-12",
		IssueTitle:  "Avoid eval",
		TLDR:        "eval runs arbitrary code",
		HowToFix:    "Parse the input instead",
		Language:    ".NET",
		Priority:    50,
		HasError:    true,
	}

	model := customSastRuleModelFromAPI(rule)

	want := customSastRuleModel{
		ID:          types.StringValue("12"),
		SemgrepRule: types.StringValue("rules:\n  - id: CUSTOM-RULE-12"),
		IssueTitle:  types.StringValue("Avoid eval"),
		TLDR:        types.StringValue("eval runs arbitrary code"),
		HowToFix:    types.StringValue("Parse the input instead"),
		Language:    types.StringValue(".NET"),
		Priority:    types.Int64Value(50),
		HasError:    types.BoolValue(true),
	}
	if model != want {
		t.Errorf("model = %+v, want %+v", model, want)
	}
}

func TestCustomSastRuleModels(t *testing.T) {
	// A null list would break for expressions over rules in configurations.
	t.Run("no rules yields an empty non-nil list", func(t *testing.T) {
		models := customSastRuleModels(nil)
		if models == nil || len(models) != 0 {
			t.Errorf("models = %#v, want an empty non-nil slice", models)
		}
	})

	t.Run("rules keep the order they are given in", func(t *testing.T) {
		models := customSastRuleModels([]customrules.Rule{{ID: 3}, {ID: 7}})

		if len(models) != 2 || models[0].ID != types.StringValue("3") || models[1].ID != types.StringValue("7") {
			t.Errorf("models = %+v, want IDs 3 then 7", models)
		}
	})
}
