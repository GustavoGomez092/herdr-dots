package credentials

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// S12 (post-rc hardening): Last4 counts characters, not bytes, so a key from
// the environment with non-ASCII characters never shows a split character.
func TestLast4NeverSplitsACharacter(t *testing.T) {
	got := Last4("abcdefghijk-ñé€ü")
	if !utf8.ValidString(got) || got != "…ñé€ü" {
		t.Fatalf("Last4 = %q, want …ñé€ü", got)
	}
	if got := Last4("short€€€€€"); got != "" {
		t.Fatalf("a key under 12 characters shows nothing, got %q", got)
	}
	if got := Last4("sk-ant-api03-abcd1234"); got != "…1234" {
		t.Fatalf("ASCII key: %q", got)
	}
}

// S13 (post-rc hardening): a credential in a query string is redacted by its
// parameter name too, not only by key shape.
func TestRedactRemovesQueryStringCredentialsByName(t *testing.T) {
	for _, secret := range []string{"key=opaquegooglekey123", "token=opaquetoken4567", "auth=opaqueauth89012",
		"secret=opaquesecret345", "client_secret=opaqueclient678", "password=hunter2hunter2", "sig=opaquesignature9"} {
		for _, sep := range []string{"?", "&"} {
			in := "GET https://api.example/v1/run" + sep + secret + "&x=1 failed"
			value := secret[strings.Index(secret, "=")+1:]
			out := Redact(in)
			if strings.Contains(out, value) || !strings.Contains(out, Redacted) || !strings.Contains(out, "&x=1 failed") {
				t.Errorf("Redact(%q) = %q", in, out)
			}
		}
	}
	// Ordinary text with the same words is left alone.
	for _, keep := range []string{"the key=value pairs", "a token=short", "monkey=bananas123"} {
		if out := Redact(keep); out != keep {
			t.Errorf("Redact(%q) = %q, want it unchanged", keep, out)
		}
	}
}
