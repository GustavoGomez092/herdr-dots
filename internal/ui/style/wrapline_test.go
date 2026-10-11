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

// M4: a line never vanishes, and dropping an indent keeps the colour that
// starts the line.
func TestWrapLineNeverDropsALineOrItsColour(t *testing.T) {
	if got := WrapLine(strings.Repeat(" ", 30), 10); len(got) != 1 {
		t.Fatalf("a space-only line vanished: %q", got)
	}
	got := WrapLine("\x1b[31m   "+strings.Repeat("x", 12)+"\x1b[0m", 10)
	if !strings.HasPrefix(got[0], "\x1b[31m") {
		t.Fatalf("the colour start was dropped with the indent: %q", got)
	}
}

func TestWrapExactGivesTheLineBack(t *testing.T) {
	for _, s := range []string{"", strings.Repeat(" ", 25), "  a  b   " + strings.Repeat("界", 9) + "  end  ", "x"} {
		for _, w := range []int{1, 3, 7, 10} {
			rows := WrapExact(s, w)
			if strings.Join(rows, "") != s {
				t.Fatalf("w=%d %q: rows %q", w, s, rows)
			}
			for _, r := range rows {
				if ansi.StringWidth(r) > max(w, 2) {
					t.Fatalf("w=%d: row %q too wide", w, r)
				}
			}
		}
	}
}

func TestWrapExactWordsBreaksAfterSpacesAndGivesTheLineBack(t *testing.T) {
	s := "Will run: claude mcp add --env 'A=${A:-}'  --transport stdio " + strings.Repeat("y", 30)
	rows := WrapExactWords(s, 20)
	if strings.Join(rows, "") != s {
		t.Fatalf("rows %q", rows)
	}
	if rows[0] != "Will run: claude " || rows[1] != "mcp add --env " {
		t.Fatalf("rows %q: want breaks after spaces", rows)
	}
	for _, r := range rows {
		if ansi.StringWidth(r) > 20 {
			t.Fatalf("row %q too wide", r)
		}
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
