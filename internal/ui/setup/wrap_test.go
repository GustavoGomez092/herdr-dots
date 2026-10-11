package setup

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// I3: plan text (diffs, TOML) is wrapped byte for byte: the rows, without
// their indent, give the line back exactly, every space included.
func TestPlanLinesKeepEverySpace(t *testing.T) {
	for _, s := range []string{
		"+ args    =    [\"" + strings.Repeat(" ", 40) + "--state-dir\", \"/state\"]",
		strings.Repeat(" ", 30),
		"prefix+shift+c  herdr-dots: council (ask every dot)",
		"Will run: claude mcp add --scope user --env 'A=${A:-}'   --transport stdio herdr-dots",
	} {
		rows := hard(s, 16, 2)
		var got strings.Builder
		for _, r := range rows {
			if !strings.HasPrefix(r, "  ") || ansi.StringWidth(r) > 16 {
				t.Fatalf("%q: row %q not indented or wider than 16", s, r)
			}
			got.WriteString(r[2:])
		}
		if got.String() != s {
			t.Fatalf("rows %q don't give back %q", rows, s)
		}
	}
}

// B5: the long `claude mcp add` line breaks at spaces, never inside an
// argument, and every piece keeps the change indent.
func TestTheCommandLineBreaksAtSpaces(t *testing.T) {
	const w = 44
	cmd := nothingInstalled().Clients[0].Change[1]
	lines := Screen{Offer: nothingInstalled(), Forced: true, IncludeKeys: true}.Lines(w)
	start, end := -1, -1
	for i, l := range lines {
		l = ansi.Strip(l)
		if strings.Contains(l, "claude mcp add") {
			start = i
		}
		if start >= 0 && end < 0 && strings.Contains(l, "stores this under") {
			end = i
		}
	}
	if start < 0 || end < 0 {
		t.Fatalf("command not found:\n%s", strings.Join(lines, "\n"))
	}
	piece := lines[start:end]
	for _, l := range piece {
		if ansi.StringWidth(l) > w || !strings.HasPrefix(l, "  ") {
			t.Errorf("line %q: wider than %d or not indented", l, w)
		}
	}
	if got := strings.Fields(strings.Join(piece, " ")); !slices.Equal(got, strings.Fields(cmd)) {
		t.Fatalf("the command was broken inside an argument:\n%s", strings.Join(piece, "\n"))
	}
}
