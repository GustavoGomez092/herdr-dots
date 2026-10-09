package textutil

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzSanitizeModelText checks the filter every model text passes before it
// reaches a terminal, a prompt or storage, against Unicode's own tables (not
// the function's list): the output is the input minus every control
// character but newline and tab, and minus every Bidi_Control character, so
// nothing can rewrite the screen or reorder what it shows; and sanitizing
// twice changes nothing. Other format characters (Cf) pass through on
// purpose: the zero width joiner keeps emoji sequences whole (Phase 2's
// TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners), and the secret scanner
// strips invisible characters itself before matching.
func FuzzSanitizeModelText(f *testing.F) {
	for _, s := range []string{
		"plain", "a\nb\tc", "\x1b[2Jgone\x07", "\x1b]52;c;aGk=\x07", "\u0085\u009b31m", "‮evil‬",
		"⁦x⁩‎‏؜", "👩‍💻 ok️", "\xff\xfe broken utf-8", "\r\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := SanitizeModelText(s)
		want := strings.Map(func(r rune) rune {
			if r != '\n' && r != '\t' && (unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Bidi_Control, r)) {
				return -1
			}
			return r
		}, s)
		if out != want {
			t.Fatalf("SanitizeModelText(%q) = %q, want %q", s, out, want)
		}
		if again := SanitizeModelText(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, out, again)
		}
	})
}
