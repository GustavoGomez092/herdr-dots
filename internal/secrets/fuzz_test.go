package secrets

import (
	"strings"
	"testing"

	"herdr-dots/internal/credentials"
)

// FuzzSecretsScan puts a fixed, valid key of each shape between arbitrary
// text: Scan must always find it, and credentials.Redact (which scrubs logs,
// the feed, toasts, snapshots and error strings) must never leave it. The
// key is set off from the fuzz text by a space where the text would
// otherwise run into it (a word character on either side), because a key
// glued to other word characters is a longer token, not that key.
// Google API keys are not a credentials provider and Redact does not claim
// them, so they are checked against Scan only.
func FuzzSecretsScan(f *testing.F) {
	keys := []struct {
		key    string
		redact bool
	}{
		{"sk-ant-api03-Zq8xR2vT9mK4pL7nW3bY6cH1dF5gJ0sA", true},
		{"sk-proj-Tn4Lp9Qw2Xc7Vb5Mz8Ka3Hd6Jf1Gs0Ry", true},
		{"ghp_aB3dE5fG7hJ9kL1mN2pQ4rS6tU8vW0xY2zA4", true},
		{"github_pat_11ABCDEFG0123456789_abcdefghijKLMNOP", true},
		{"AKIAQ7XK2M9P4R8T1V5W", true},
		{"xoxb-2741963580-Kq7Rt2Vw9Xz4", true},
		{"sk_live_4eC39HqLyjWDarjtT1zdp7dc", true},
		{"glpat-xYz123ABCdef456GHIjkl", true},
		{"AIzaSyD4kR7tW2qX9mN3bV8cL1pF6hJ0gZ5aE2u", false},
	}
	for i := range keys {
		f.Add("export KEY=", uint8(i), "\n")
		f.Add("Authorization: Bearer", uint8(i), "")
		f.Add("", uint8(i), "_tail")
	}
	f.Add("api_key=\"", uint8(0), "\"")
	f.Add("​\xff", uint8(4), "9")
	word := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	f.Fuzz(func(t *testing.T, prefix string, pick uint8, suffix string) {
		k := keys[int(pick)%len(keys)]
		if prefix != "" && word(prefix[len(prefix)-1]) {
			prefix += " "
		}
		if suffix != "" && word(suffix[0]) {
			suffix = " " + suffix
		}
		text := prefix + k.key + suffix
		if found, kind := Scan(text); !found || kind == "" {
			t.Fatalf("Scan missed %q in %q", k.key, text)
		}
		if out := credentials.Redact(text); k.redact && strings.Contains(out, k.key) {
			t.Fatalf("Redact(%q) = %q still holds the key", text, out)
		}
	})
}
