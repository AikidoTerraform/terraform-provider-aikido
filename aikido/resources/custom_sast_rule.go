package resources

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/customrules"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &customSastRuleResource{}
	_ resource.ResourceWithImportState = &customSastRuleResource{}
	_ resource.ResourceWithConfigure   = &customSastRuleResource{}
)

func NewCustomSastRuleResource() resource.Resource {
	return &customSastRuleResource{}
}

// semgrepRuleHeader mirrors the first check Aikido runs on a rule, so a rule it
// would reject fails at plan rather than at apply. CRLF is allowed because the
// provider converts it before sending.
var semgrepRuleHeader = regexp.MustCompile(`^rules:(\r?\n|$)`)

type customSastRuleResource struct {
	client *client.Client
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

func (r *customSastRuleResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_custom_sast_rule"
}

func (r *customSastRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages a custom SAST rule: a semgrep rule Aikido runs on every code repository scan, " +
			"raising an issue with the given title, summary and fix instructions for each match. " +
			"Custom rules require a Pro plan or higher; free and trial workspaces are limited to 5 rules. " +
			"The API client needs the custom_sast_rules:read and custom_sast_rules:write scopes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido custom rule ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"semgrep_rule": schema.StringAttribute{
				Required: true,
				Description: "The semgrep rule, in YAML. It must define a single rule: the first line must be exactly `rules:`, " +
					"every other line must be indented, and the rule needs id, message, languages, severity and a pattern. " +
					"Trailing newlines and Windows line endings are removed before sending, so heredocs and file() work as-is. " +
					"Aikido replaces the rule id with CUSTOM-RULE-<id>; the provider accounts for that, so it causes no diff.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(semgrepRuleHeader, "must start with a first line that is exactly \"rules:\""),
				},
			},
			"issue_title": schema.StringAttribute{
				Required:    true,
				Description: "Title of the issues this rule raises.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"tldr": schema.StringAttribute{
				Required:    true,
				Description: "Short explanation of the issue, shown on each issue this rule raises.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"how_to_fix": schema.StringAttribute{
				Required:    true,
				Description: "Instructions for fixing the issue, shown on each issue this rule raises.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"language": schema.StringAttribute{
				Required:    true,
				Description: "Language the rule targets. Case-sensitive; one of " + strings.Join(customrules.Languages, ", ") + ".",
				Validators: []validator.String{
					stringvalidator.OneOf(customrules.Languages...),
				},
			},
			"priority": schema.Int64Attribute{
				Required:    true,
				Description: "Priority of the issues this rule raises, from 1 to 100.",
				Validators: []validator.Int64{
					int64validator.Between(1, 100),
				},
			},
			"has_error": schema.BoolAttribute{
				Computed: true,
				Description: "Whether an Aikido scan failed on this rule since it was last saved. " +
					"Saving the rule clears it until the next scan.",
			},
		},
	}
}

func (r *customSastRuleResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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
	r.client = apiClient
}

func (r *customSastRuleResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned customSastRuleModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := customrules.Create(ctx, r.client, customSastRuleWrite(planned))
	if err != nil {
		response.Diagnostics.AddError("Error creating custom SAST rule", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, savedCustomSastRuleModel(id, planned))...)
}

func (r *customSastRuleResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState customSastRuleModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseCustomSastRuleID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading custom SAST rule", err.Error())
		return
	}

	rule, err := customrules.Get(ctx, r.client, id)
	if err != nil {
		if client.NotFound(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading custom SAST rule", err.Error())
		return
	}

	response.Diagnostics.Append(customSastRuleErrorDiagnostics(rule)...)
	response.Diagnostics.Append(response.State.Set(ctx, customSastRuleModelFromAPI(rule, priorState.SemgrepRule))...)
}

func (r *customSastRuleResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned customSastRuleModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseCustomSastRuleID(planned.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error updating custom SAST rule", err.Error())
		return
	}

	if err := customrules.Update(ctx, r.client, id, customSastRuleWrite(planned)); err != nil {
		response.Diagnostics.AddError("Error updating custom SAST rule", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, savedCustomSastRuleModel(id, planned))...)
}

func (r *customSastRuleResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState customSastRuleModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseCustomSastRuleID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error deleting custom SAST rule", err.Error())
		return
	}

	if err := customrules.Delete(ctx, r.client, id); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deleting custom SAST rule", err.Error())
	}
}

// ImportState adopts an existing custom rule by its numeric ID. The imported
// semgrep_rule carries Aikido's CUSTOM-RULE-<id> rule id, so a configuration
// using its own rule id shows one update on the first plan after import.
func (r *customSastRuleResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

func customSastRuleWrite(planned customSastRuleModel) customrules.Write {
	return customrules.Write{
		SemgrepRule: planned.SemgrepRule.ValueString(),
		IssueTitle:  planned.IssueTitle.ValueString(),
		TLDR:        planned.TLDR.ValueString(),
		HowToFix:    planned.HowToFix.ValueString(),
		Language:    planned.Language.ValueString(),
		Priority:    planned.Priority.ValueInt64(),
	}
}

// savedCustomSastRuleModel is the state after a successful write. It skips a
// read-back: every configured field was just accepted as sent, and Aikido only
// reports scan errors raised after a rule's last save, so has_error is false.
func savedCustomSastRuleModel(id int64, planned customSastRuleModel) customSastRuleModel {
	planned.ID = types.StringValue(strconv.FormatInt(id, 10))
	planned.HasError = types.BoolValue(false)

	return planned
}

// customSastRuleModelFromAPI maps an API rule into state. Aikido stores a
// rewritten semgrep_rule, so the prior value is kept while it still produces
// what Aikido stored; anything else is a change made outside Terraform.
func customSastRuleModelFromAPI(rule customrules.Rule, priorSemgrepRule types.String) customSastRuleModel {
	semgrepRule := types.StringValue(rule.SemgrepRule)
	if !priorSemgrepRule.IsNull() && customrules.StoredSemgrepRule(priorSemgrepRule.ValueString(), rule.ID) == rule.SemgrepRule {
		semgrepRule = priorSemgrepRule
	}

	return customSastRuleModel{
		ID:          types.StringValue(strconv.FormatInt(rule.ID, 10)),
		SemgrepRule: semgrepRule,
		IssueTitle:  types.StringValue(rule.IssueTitle),
		TLDR:        types.StringValue(rule.TLDR),
		HowToFix:    types.StringValue(rule.HowToFix),
		Language:    types.StringValue(rule.Language),
		Priority:    types.Int64Value(rule.Priority),
		HasError:    types.BoolValue(rule.HasError),
	}
}

// customSastRuleErrorDiagnostics surfaces a failed scan on plan, since Aikido
// otherwise only shows it in the dashboard and the rule silently finds nothing.
func customSastRuleErrorDiagnostics(rule customrules.Rule) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	if !rule.HasError {
		return diagnostics
	}

	diagnostics.AddWarning(
		"Custom SAST rule failed during a scan",
		fmt.Sprintf("Custom rule %d (%q) caused an error during an Aikido scan, so it is not finding issues. "+
			"Check the rule in the Aikido dashboard and fix the semgrep_rule; saving it clears the error until the next scan.",
			rule.ID, rule.IssueTitle),
	)

	return diagnostics
}

func parseCustomSastRuleID(ruleID string) (int64, error) {
	id, err := strconv.ParseInt(ruleID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid custom rule id %q: %w", ruleID, err)
	}

	return id, nil
}
