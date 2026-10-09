package credentials

import (
	"strings"
	"testing"
)

// \b does not match after "_" or a digit, so a key glued to a variable name
// or an id used to pass through.
func TestRedactKeysAfterUnderscoreOrDigit(t *testing.T) {
	a40 := strings.Repeat("aB3", 14)[:40]
	a48 := strings.Repeat("xY7", 16)
	cases := map[string]struct{ in, secret string }{
		"proj":        {"OPENAI_KEY_sk-proj-" + a40, a40},
		"plain 48":    {"sk-" + a48, a48},
		"underscore":  {"x_sk-" + a48, a48},
		"digit":       {"9sk-" + a48, a48},
		"ghp glued":   {"TOKEN_ghp_" + strings.Repeat("a", 36), strings.Repeat("a", 36)},
		"aws glued":   {"id_AKIA" + strings.Repeat("B", 16), strings.Repeat("B", 16)},
		"slack glued": {"SLACK_xoxb-1234567890-abcdef", "1234567890-abcdef"},
		"stripe":      {"STRIPE_sk_live_" + strings.Repeat("c", 24), strings.Repeat("c", 24)},
		"gitlab":      {"GL_glpat-" + strings.Repeat("d", 20), strings.Repeat("d", 20)},
		"url":         {"x_https://bob:hunter2pass@example.com", "hunter2pass"},
	}
	for name, c := range cases {
		out := Redact(c.in)
		if strings.Contains(out, c.secret) || !strings.Contains(out, Redacted) {
			t.Errorf("%s: not redacted: %q", name, out)
		}
	}
	// No false positive: "sk-" inside a word keeps its text.
	if got := Redact("the task-management-system-list is fine"); got != "the task-management-system-list is fine" {
		t.Errorf("false positive: %q", got)
	}
}
