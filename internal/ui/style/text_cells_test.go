package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapAndCutCountDisplayCells(t *testing.T) {
	in := strings.Repeat("漢字", 12) + " " + strings.Repeat("😀", 9) + " end"
	for _, w := range []int{10, 15, 40} {
		for _, l := range Wrap(in, w) {
			if ansi.StringWidth(l) > w {
				t.Errorf("w=%d line %q is %d cells", w, l, ansi.StringWidth(l))
			}
		}
	}
	if got := Cut("漢字漢字漢字", 7); ansi.StringWidth(got) > 7 || !strings.HasSuffix(got, "…") {
		t.Fatalf("cut %q", got)
	}
	if Cut("short", 10) != "short" {
		t.Fatal("short strings are untouched")
	}
}

func TestSeparatorsHaveASCIIStandIns(t *testing.T) {
	old := SetASCII(true)
	defer SetASCII(old)
	for _, s := range []string{"·", "─", "—", "▸", "…"} {
		if got := Sym(s); strings.ContainsAny(got, "·─—▸…") || got == "*" {
			t.Errorf("Sym(%q) = %q", s, got)
		}
	}
	if got := Cut("漢字漢字漢字", 7); ansi.StringWidth(got) > 7 || strings.Contains(got, "…") {
		t.Fatalf("ascii cut %q", got)
	}
}
