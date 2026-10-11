package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapLineBreaksAtSpacesOnly(t *testing.T) {
	got := WrapLine("claude mcp add --scope user --env 'A=${A:-}' --transport stdio herdr-dots", 20)
	want := []string{"claude mcp add", "--scope user --env", "'A=${A:-}'", "--transport stdio", "herdr-dots"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestWrapLineKeepsIndentAndAlignment(t *testing.T) {
	got := WrapLine("  prefix+d        herdr-dots: manage dots", 30)
	want := []string{"  prefix+d        herdr-dots:", "manage dots"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if got := WrapLine("  short", 30); len(got) != 1 || got[0] != "  short" {
		t.Fatalf("a line that fits is unchanged: %q", got)
	}
}

func TestWrapLineHardBreaksOnlyAWordWiderThanTheLine(t *testing.T) {
	got := WrapLine("ab "+strings.Repeat("x", 25)+" cd", 10)
	want := []string{"ab", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx cd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestWrapLineKeepsANSIAndCountsCells(t *testing.T) {
	in := "\x1b[31monboarding.json is damaged: fix or delete it\x1b[0m"
	got := WrapLine(in, 20)
	if ansi.Strip(strings.Join(got, " ")) != ansi.Strip(in) {
		t.Fatalf("got %q", got)
	}
	for _, l := range got {
		if ansi.StringWidth(l) > 20 {
			t.Fatalf("line %q wider than 20", l)
		}
	}
	if !strings.HasPrefix(got[0], "\x1b[31m") {
		t.Fatalf("the colour is kept: %q", got)
	}
}
