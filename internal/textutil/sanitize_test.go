package textutil

import "testing"

func TestSanitizeModelTextDropsControlsKeepsNewlineTab(t *testing.T) {
	cases := map[string]string{
		"plain":                        "plain",
		"line1\nline2\tcol":            "line1\nline2\tcol",
		"a\u001b]52;c;ZXZpbA==\u0007b": "a]52;c;ZXZpbA==b",
		"x\u009b31my":                  "x31my",
		"\u001b[2J\u001b[Hcleared\r":   "[2J[Hcleared",
		"ünïcødé ✓ \u0000nul":          "ünïcødé ✓ nul",
	}
	for in, want := range cases {
		if got := SanitizeModelText(in); got != want {
			t.Errorf("SanitizeModelText(%q) = %q, want %q", in, got, want)
		}
	}
}
