package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// reversedCells walks line's SGR sequences and reports, for each visible
// rune, whether reverse video is on there.
func reversedCells(line string) (text string, rev []bool) {
	on := false
	var b strings.Builder
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], "\x1b[") {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				params := line[i+2 : j]
				if params == "" {
					on = false
				}
				for _, p := range strings.Split(params, ";") {
					switch p {
					case "0":
						on = false
					case "7":
						on = true
					case "27":
						on = false
					}
				}
			}
			i = j + 1
			continue
		}
		r := []rune(line[i:])[0]
		b.WriteRune(r)
		rev = append(rev, on)
		i += len(string(r))
	}
	return b.String(), rev
}

// B4: the selected roster row is highlighted whole, not just its glyph (the
// glyph's own colour reset used to end the reverse video).
func TestTheSelectedRosterRowIsHighlightedWhole(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	m := press(ready(&fakeCaller{}), "tab") // the roster, Security selected
	var row string
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "thinking") {
			row = l
		}
	}
	if row == "" {
		t.Fatalf("no roster row for Security:\n%s", m.View())
	}
	text, rev := reversedCells(row)
	if !strings.Contains(text, "Security") {
		t.Fatalf("row %q", text)
	}
	for i, r := range []rune(text) {
		if r != ' ' && !rev[i] {
			t.Fatalf("%q at cell %d of %q is not highlighted (raw %q)", r, i, text, row)
		}
	}
}
