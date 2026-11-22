// Package customrules models Aikido custom SAST (semgrep) rules and centralizes
// their API calls, along with the rewriting Aikido applies to a stored rule, so
// the resource and data source agree on what a rule looks like once saved.
package customrules

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

const BasePath = "/public/v1/repositories/sast/custom-rules"

// Languages are the values the API accepts for language. The API docs omit VB
// and yaml, which the server accepts. Matching is case-sensitive.
var Languages = []string{
	"JS", "TS", "PHP", "Java", "Scala", "GO", "PY", "Ruby", ".NET", "RUST", "Dart",
	"Swift", "Elixir", "C", "Clojure", "kotlin", "terraform", "VB", "yaml", "generic",
}

// Rule is a custom rule as returned by the list and detail endpoints.
// HasError reports whether a scan failed on the rule since it was last saved.
type Rule struct {
	ID          int64  `json:"id"`
	SemgrepRule string `json:"semgrep_rule"`
	IssueTitle  string `json:"issue_title"`
	TLDR        string `json:"tldr"`
	HowToFix    string `json:"how_to_fix"`
	Language    string `json:"language"`
	Priority    int64  `json:"priority"`
	HasError    bool   `json:"has_error"`
}

// Write is the body of the create and edit endpoints. Edit is a full
// replacement, so every field is always sent.
type Write struct {
	SemgrepRule string `json:"semgrep_rule"`
	IssueTitle  string `json:"issue_title"`
	TLDR        string `json:"tldr"`
	HowToFix    string `json:"how_to_fix"`
	Language    string `json:"language"`
	Priority    int64  `json:"priority"`
}

// idLinePattern mirrors the server-side rewrite, which replaces the value of
// every "- id: " occurrence with the rule's own unique ID.
var idLinePattern = regexp.MustCompile(`- id: [^\n]+`)

// DetailPath is the path of a single custom rule.
func DetailPath(id int64) string {
	return BasePath + "/" + strconv.FormatInt(id, 10)
}

// NormalizeSemgrepRule makes a rule acceptable to the API. The server rejects
// any line that is not indented, including the empty last line a heredoc or
// file() leaves behind, and a carriage return breaks its "rules:" header check.
func NormalizeSemgrepRule(rule string) string {
	return strings.TrimRight(strings.ReplaceAll(rule, "\r\n", "\n"), "\n")
}

// StoredSemgrepRule predicts how Aikido stores a rule sent for the given ID:
// normalized, with the user's rule id replaced by CUSTOM-RULE-<id>.
func StoredSemgrepRule(rule string, id int64) string {
	return idLinePattern.ReplaceAllLiteralString(NormalizeSemgrepRule(rule), "- id: CUSTOM-RULE-"+strconv.FormatInt(id, 10))
}

// All returns every custom rule in the workspace, sorted by ID so Terraform
// sees a stable order across plans. The endpoint is not paginated.
func All(ctx context.Context, apiClient *client.Client) ([]Rule, error) {
	var listed struct {
		CustomRules []Rule `json:"custom_rules"`
	}
	if err := apiClient.Do(ctx, http.MethodGet, BasePath, nil, &listed); err != nil {
		return nil, err
	}

	slices.SortFunc(listed.CustomRules, func(left, right Rule) int {
		return cmp.Compare(left.ID, right.ID)
	})

	return listed.CustomRules, nil
}

// Get reads one custom rule. A rule that does not exist is a 404.
func Get(ctx context.Context, apiClient *client.Client, id int64) (Rule, error) {
	var detail struct {
		CustomRule Rule `json:"custom_rule"`
	}
	if err := apiClient.Do(ctx, http.MethodGet, DetailPath(id), nil, &detail); err != nil {
		return Rule{}, err
	}

	return detail.CustomRule, nil
}

// Create adds a custom rule and returns its new ID.
func Create(ctx context.Context, apiClient *client.Client, write Write) (int64, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	if err := apiClient.Do(ctx, http.MethodPost, BasePath, normalized(write), &created); err != nil {
		return 0, err
	}
	// A zero ID would be saved to state, read back as a 404 and dropped, so every
	// apply would create another rule while Terraform tracks none of them.
	if created.ID == 0 {
		return 0, fmt.Errorf("empty id in create custom rule response")
	}

	return created.ID, nil
}

// Update replaces every field of a custom rule.
func Update(ctx context.Context, apiClient *client.Client, id int64, write Write) error {
	return apiClient.Do(ctx, http.MethodPut, DetailPath(id), normalized(write), nil)
}

// Delete removes a custom rule, along with the issues Aikido raised for it.
func Delete(ctx context.Context, apiClient *client.Client, id int64) error {
	return apiClient.Do(ctx, http.MethodDelete, DetailPath(id), nil, nil)
}

func normalized(write Write) Write {
	write.SemgrepRule = NormalizeSemgrepRule(write.SemgrepRule)

	return write
}
