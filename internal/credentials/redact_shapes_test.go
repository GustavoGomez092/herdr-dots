package credentials

import (
	"strings"
	"testing"
)

func TestRedactCoversScanSecretsShapes(t *testing.T) {
	cases := map[string]string{
		"ghp":        "token ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789 end",
		"gho":        "gho_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
		"ghs":        "ghs_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
		"ghu":        "ghu_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
		"github_pat": "github_pat_11ABCDEFG0123456789_abcdefghijKLMNOP",
		"akia":       "key AKIAIOSFODNN7EXAMPLE here",
		"asia":       "key ASIAIOSFODNN7EXAMPLE here",
		"slack":      "bot xoxb-1234567890-abcdefghij",
		"stripe":     "sk_live_4eC39HqLyjWDarjtT1zdp7dc",
		"stripe rk":  "rk_live_4eC39HqLyjWDarjtT1zdp7dc",
		"gitlab":     "glpat-xYz123ABCdef456GHIjkl",
		"url creds":  "dsn postgres://app:Sup3rS3cretPw@db:5432/x",
	}
	secrets := []string{"aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", "11ABCDEFG0123456789", "IOSFODNN7EXAMPLE", "1234567890-abcdefghij", "4eC39HqLyjWDarjtT1zdp7dc", "xYz123ABCdef456GHIjkl", "Sup3rS3cretPw"}
	for name, in := range cases {
		out := Redact(in)
		if !strings.Contains(out, Redacted) {
			t.Errorf("%s: not redacted: %q", name, out)
		}
		for _, secret := range secrets {
			if strings.Contains(out, secret) {
				t.Errorf("%s: secret %q survives: %q", name, secret, out)
			}
		}
	}
}

func TestRedactShapesNoFalsePositives(t *testing.T) {
	for _, s := range []string{
		"task-ghp is done", "AKIA123 short", "ghp_short", "see https://example.com/a@b",
		"http://localhost:8080/docs", "glpat-short", "xoxb-1", "sk_live_short",
	} {
		if out := Redact(s); out != s {
			t.Errorf("Redact(%q) = %q", s, out)
		}
	}
}
