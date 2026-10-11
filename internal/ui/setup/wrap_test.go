package setup

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
