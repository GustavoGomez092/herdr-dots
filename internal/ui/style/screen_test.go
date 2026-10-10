package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// What the developer confirms is fully on screen, measured in display cells
// (Task 10's rule, shared by memui's approve screen and the Setup screen).
func TestScreenFitsMeasuresDisplayCellsAndTheTail(t *testing.T) {
	lines := []string{"title", strings.Repeat("漢", 15)} // 30 cells
	if !ScreenFits(lines, 30, 5, 3) {
		t.Fatal("2 lines + 3 tail in 5 rows, 30 cells in 30 columns: fits")
	}
	if ScreenFits(lines, 29, 5, 3) {
		t.Fatal("a 30-cell line in 29 columns does not fit")
	}
	if ScreenFits(lines, 30, 4, 3) {
		t.Fatal("2 lines + 3 tail in 4 rows does not fit")
	}
	if !ScreenFits(lines, 30, 0, 3) {
		t.Fatal("an unknown height fits")
	}
	if ScreenFits([]string{"x"}, MinScreenWidth-1, 40, 3) {
		t.Fatal("narrower than MinScreenWidth never fits")
	}
}

func TestTooSmallSaysWhyInDisplayCells(t *testing.T) {
	got := TooSmall("SETUP", 7, 30, 8, "setup screen", "install")
	if !strings.Contains(ansi.Strip(got[0]), "SETUP") {
		t.Fatalf("title %q", got[0])
	}
	text := strings.Join(got[1:], " ")
	if !strings.Contains(text, "Too small to show the whole setup screen (needs 7 lines, has 8). Enlarge the popup to install.") {
		t.Fatalf("text %q", text)
	}
	for _, l := range got[1:] {
		if ansi.StringWidth(l) > 30 {
			t.Fatalf("line %q wider than 30", l)
		}
	}
	narrow := strings.Join(TooSmall("SETUP", 7, 20, 8, "setup screen", "install"), " ")
	if !strings.Contains(narrow, "needs 24 columns, has 20") {
		t.Fatalf("narrow %q", narrow)
	}
	// A long title wraps too: every line of the screen fits.
	for _, l := range TooSmall("Let your agent consult your dots?", 40, 30, 8, "setup screen", "install") {
		if ansi.StringWidth(l) > 30 {
			t.Fatalf("line %q wider than 30", l)
		}
	}
}
