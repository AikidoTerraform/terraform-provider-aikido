package datasources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/customrules"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &customSastRulesDataSource{}
	_ datasource.DataSourceWithConfigure = &customSastRulesDataSource{}
)

func NewCustomSastRulesDataSource() datasource.DataSource {
	return &customSastRulesDataSource{}
}

type customSastRulesDataSource struct {
	client *client.Client
}

type customSastRulesDataSourceModel struct {
	Rules []customSastRuleModel `tfsdk:"rules"`
}

type customSastRuleModel struct {
	ID          types.String `tfsdk:"id"`
	SemgrepRule types.String `tfsdk:"semgrep_rule"`
	IssueTitle  types.String `tfsdk:"issue_title"`
	TLDR        types.String `tfsdk:"tldr"`
	HowToFix    types.String `tfsdk:"how_to_fix"`
	Language    types.String `tfsdk:"language"`
	Priority    types.Int64  `tfsdk:"priority"`
	HasError    types.Bool   `tfsdk:"has_error"`
}

func (d *customSastRulesDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_custom_sast_rules"
}

func (d *customSastRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Lists every custom SAST rule in the workspace, whether created in Terraform or in the Aikido dashboard. " +
			"Use it to find the ID of a rule to import into aikido_custom_sast_rule, or to check which rules failed during a scan. " +
			"The API client needs the custom_sast_rules:read scope.",
		Attributes: map[string]schema.Attribute{
			"rules": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Custom rules, ordered by Aikido custom rule ID.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Aikido custom rule ID, as a string to match the id attribute of aikido_custom_sast_rule.",
						},
						"semgrep_rule": schema.StringAttribute{
							Computed:    true,
							Description: "The semgrep rule as Aikido stores it, with its rule id replaced by CUSTOM-RULE-<id>.",
						},
						"issue_title": schema.StringAttribute{
							Computed:    true,
							Description: "Title of the issues this rule raises.",
						},
						"tldr": schema.StringAttribute{
							Computed:    true,
							Description: "Short explanation of the issue.",
						},
						"how_to_fix": schema.StringAttribute{
							Computed:    true,
							Description: "Instructions for fixing the issue.",
						},
						"language": schema.StringAttribute{
							Computed:    true,
							Description: "Language the rule targets.",
						},
						"priority": schema.Int64Attribute{
							Computed:    true,
							Description: "Priority of the issues this rule raises, from 1 to 100.",
						},
						"has_error": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether an Aikido scan failed on this rule since it was last saved.",
						},
					},
				},
			},
		},
	}
}

func (d *customSastRulesDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
	if request.ProviderData == nil {
		return
	}
	apiClient, isClient := request.ProviderData.(*client.Client)
	if !isClient {
		response.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", request.ProviderData),
		)
		return
	}
	d.client = apiClient
}

func (d *customSastRulesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, response *datasource.ReadResponse) {
	allRules, err := customrules.All(ctx, d.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading custom SAST rules", err.Error())
		return
	}

	state := customSastRulesDataSourceModel{Rules: customSastRuleModels(allRules)}
	response.Diagnostics.Append(response.State.Set(ctx, &state)...)
}

// customSastRuleModels is always non-nil, so that a workspace without rules
// yields an empty list rather than null.
func customSastRuleModels(rules []customrules.Rule) []customSastRuleModel {
	models := make([]customSastRuleModel, 0, len(rules))
	for _, rule := range rules {
		models = append(models, customSastRuleModelFromAPI(rule))
	}

	return models
}

func customSastRuleModelFromAPI(rule customrules.Rule) customSastRuleModel {
	return customSastRuleModel{
		ID:          types.StringValue(strconv.FormatInt(rule.ID, 10)),
		SemgrepRule: types.StringValue(rule.SemgrepRule),
		IssueTitle:  types.StringValue(rule.IssueTitle),
		TLDR:        types.StringValue(rule.TLDR),
		HowToFix:    types.StringValue(rule.HowToFix),
		Language:    types.StringValue(rule.Language),
		Priority:    types.Int64Value(rule.Priority),
		HasError:    types.BoolValue(rule.HasError),
	}
}
