package memory

import (
	"strings"
	"testing"

	"herdr-dots/internal/credentials"
)

// Every provider-shaped sample ScanSecrets detects must also be scrubbed by
// credentials.Redact, so a draft summary cannot persist one verbatim.
func TestRedactCoversProviderShapedScanSecretsSamples(t *testing.T) {
	samples := map[string]string{
		"aws access key":  "AKIAIOSFODNN7EXAMPLE",
		"github token":    "ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
		"github pat":      "github_pat_11ABCDEFG0123456789_abcdefghijKLMNOP",
		"gitlab token":    "glpat-xYz123ABCdef456GHIjkl",
		"slack token":     "xoxb-1234567890-abcdefghij",
		"stripe key":      "sk_live_4eC39HqLyjWDarjtT1zdp7dc",
		"url credentials": "postgres://app:Sup3rS3cretPw@db:5432/x",
	}
	for kind, s := range samples {
		if found, _ := ScanSecrets(s); !found {
			t.Fatalf("%s sample not detected by ScanSecrets", kind)
		}
		in := "x " + s + " y"
		if out := credentials.Redact(in); strings.Contains(out, s) || out == in {
			t.Errorf("%s: Redact left %q", kind, out)
		}
	}
}
