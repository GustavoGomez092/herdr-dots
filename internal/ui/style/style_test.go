package style

import (
	"strings"
	"testing"
)

func TestSymIsIdentityByDefault(t *testing.T) {
	if ASCII() {
		t.Skip("HERDR_DOTS_ASCII is set in this environment")
	}
	for _, s := range []string{"◆", "▲", "✎", "⊕", "◌", "?", "★"} {
		if Sym(s) != s {
			t.Errorf("Sym(%q) = %q", s, Sym(s))
		}
	}
}

func TestASCIIModeMapsEveryGlyph(t *testing.T) {
	old := SetASCII(true)
	defer SetASCII(old)
	if !ASCII() {
		t.Fatal("ASCII() must report the switch")
	}
	for in, want := range map[string]string{"◆": "*", "▲": "^", "✎": "~", "⊕": "+", "◌": "o", "★": "*", "▼": "v", "✓": "+", "…": "...", "☾": "(", "?": "?", "S": "S", "☂": "*"} {
		if got := Sym(in); got != want {
			t.Errorf("Sym(%q) = %q, want %q", in, got, want)
		}
	}
	if g := Glyph("◆", "red"); !strings.Contains(g, "*") || strings.Contains(g, "◆") {
		t.Fatalf("Glyph = %q", g)
	}
}
