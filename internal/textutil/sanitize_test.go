package textutil

import "testing"

func TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners(t *testing.T) {
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0x200E, 0x200F, 0x061C} {
		in := "a" + string(r) + "b"
		if got := SanitizeModelText(in); got != "ab" {
			t.Errorf("U+%04X not removed: %q", r, got)
		}
	}
	family := "👨‍👩‍👧 ❤️"
	if got := SanitizeModelText(family); got != family {
		t.Errorf("emoji sequence changed: %q", got)
	}
}

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
