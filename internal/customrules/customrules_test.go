package customrules

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"golang.org/x/time/rate"
)

func testClient(srv *httptest.Server) *client.Client {
	return client.New(srv.Client(), srv.URL, client.WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
}

func TestNormalizeSemgrepRule(t *testing.T) {
	cases := map[string]struct {
		rule string
		want string
	}{
		"heredoc trailing newline is removed": {
			rule: "rules:\n  - id: x\n",
			want: "rules:\n  - id: x",
		},
		"several trailing newlines are removed": {
			rule: "rules:\n  - id: x\n\n\n",
			want: "rules:\n  - id: x",
		},
		"windows line endings become unix ones": {
			rule: "rules:\r\n  - id: x\r\n",
			want: "rules:\n  - id: x",
		},
		"an already clean rule is unchanged": {
			rule: "rules:\n  - id: x",
			want: "rules:\n  - id: x",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := NormalizeSemgrepRule(tc.rule); got != tc.want {
				t.Errorf("NormalizeSemgrepRule(%q) = %q, want %q", tc.rule, got, tc.want)
			}
		})
	}
}

// The expected values follow parseCustomRuleWithUniqueId in Aikido, which
// replaces the value of every "- id: " occurrence.
func TestStoredSemgrepRule(t *testing.T) {
	cases := map[string]struct {
		rule string
		want string
	}{
		"the rule id is replaced": {
			rule: "rules:\n  - id: no-eval\n    message: avoid eval\n",
			want: "rules:\n  - id: CUSTOM-RULE-12\n    message: avoid eval",
		},
		"an already rewritten id is stable": {
			rule: "rules:\n  - id: CUSTOM-RULE-12\n    message: avoid eval",
			want: "rules:\n  - id: CUSTOM-RULE-12\n    message: avoid eval",
		},
		"every occurrence is replaced, as the server does": {
			rule: "rules:\n  - id: no-eval\n    metadata:\n      refs:\n        - id: CWE-95",
			want: "rules:\n  - id: CUSTOM-RULE-12\n    metadata:\n      refs:\n        - id: CUSTOM-RULE-12",
		},
		"a rule without an id line is only normalized": {
			rule: "rules:\n  - message: avoid eval\n",
			want: "rules:\n  - message: avoid eval",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := StoredSemgrepRule(tc.rule, 12); got != tc.want {
				t.Errorf("StoredSemgrepRule(%q) = %q, want %q", tc.rule, got, tc.want)
			}
		})
	}
}

func TestAll_DecodesEnvelopeAndSortsByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != BasePath {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"custom_rules":[
			{"id":7,"semgrep_rule":"rules:","issue_title":"b","tldr":"t","how_to_fix":"h","language":"GO","priority":40,"has_error":true},
			{"id":3,"semgrep_rule":"rules:","issue_title":"a","tldr":"t","how_to_fix":"h","language":"JS","priority":10,"has_error":false}
		]}`)
	}))
	t.Cleanup(srv.Close)

	rules, err := All(context.Background(), testClient(srv))
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if len(rules) != 2 || rules[0].ID != 3 || rules[1].ID != 7 {
		t.Fatalf("rules = %+v, want IDs 3 then 7", rules)
	}
	if !rules[1].HasError || rules[1].Priority != 40 || rules[1].Language != "GO" {
		t.Errorf("rule 7 decoded as %+v", rules[1])
	}
}

func TestGet_MissingRuleIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DetailPath(99) {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"reason_phrase":"Custom rule not found or you have no access"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := Get(context.Background(), testClient(srv), 99)
	if !client.NotFound(err) {
		t.Errorf("err = %v, want a 404", err)
	}
}

func TestGet_DecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"custom_rule":{"id":5,"semgrep_rule":"rules:","issue_title":"a","tldr":"t","how_to_fix":"h","language":".NET","priority":1,"has_error":false}}`)
	}))
	t.Cleanup(srv.Close)

	rule, err := Get(context.Background(), testClient(srv), 5)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rule.ID != 5 || rule.Language != ".NET" || rule.Priority != 1 {
		t.Errorf("rule = %+v", rule)
	}
}

func TestWrites_SendNormalizedRule(t *testing.T) {
	write := Write{
		SemgrepRule: "rules:\r\n  - id: no-eval\r\n",
		IssueTitle:  "Avoid eval",
		TLDR:        "t",
		HowToFix:    "h",
		Language:    "JS",
		Priority:    50,
	}
	wantRule := "rules:\n  - id: no-eval"

	cases := map[string]struct {
		wantMethod string
		wantPath   string
		response   string
		call       func(*client.Client) error
	}{
		"create": {
			wantMethod: http.MethodPost,
			wantPath:   BasePath,
			response:   `{"id":12}`,
			call: func(apiClient *client.Client) error {
				id, err := Create(context.Background(), apiClient, write)
				if err == nil && id != 12 {
					t.Errorf("id = %d, want 12", id)
				}
				return err
			},
		},
		"update": {
			wantMethod: http.MethodPut,
			wantPath:   DetailPath(12),
			response:   `{"success":1}`,
			call: func(apiClient *client.Client) error {
				return Update(context.Background(), apiClient, 12, write)
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.wantMethod || r.URL.Path != tc.wantPath {
					t.Errorf("got %s %s, want %s %s", r.Method, r.URL.Path, tc.wantMethod, tc.wantPath)
				}

				var body Write
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decoding body: %v", err)
				}
				if body.SemgrepRule != wantRule {
					t.Errorf("semgrep_rule = %q, want %q", body.SemgrepRule, wantRule)
				}
				if body.IssueTitle != write.IssueTitle || body.Priority != write.Priority || body.Language != write.Language {
					t.Errorf("body = %+v, want the other fields sent as-is", body)
				}

				_, _ = io.WriteString(w, tc.response)
			}))
			t.Cleanup(srv.Close)

			if err := tc.call(testClient(srv)); err != nil {
				t.Fatalf("call: %v", err)
			}
		})
	}
}

func TestCreate_RejectsResponseWithoutID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := Create(context.Background(), testClient(srv), Write{}); err == nil {
		t.Fatal("Create succeeded without an id, want an error")
	}
}

func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != DetailPath(12) {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	if err := Delete(context.Background(), testClient(srv), 12); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
