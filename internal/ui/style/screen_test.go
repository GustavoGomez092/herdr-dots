package style

import (
	"slices"
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

// B6: with its tail, TooSmall fits the height, keeping the title (cut to one
// line) and the "Too small" line first.
func TestTooSmallFitsTheHeight(t *testing.T) {
	for _, h := range []int{9, 6, 4, 3} {
		got := TooSmall("Let your agent consult your dots?", 0, 20, h, 1, "setup screen", "review and install")
		if len(got)+1 > h && len(got) > 2 {
			t.Errorf("h=%d: %d lines and a tail of 1:\n%s", h, len(got), strings.Join(got, "\n"))
		}
		if !strings.HasPrefix(ansi.Strip(got[0]), "Let your agent") || !slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, "Too small") }) {
			t.Errorf("h=%d: want the title then Too small:\n%s", h, strings.Join(got, "\n"))
		}
		for _, l := range got {
			if ansi.StringWidth(l) > 20 {
				t.Errorf("h=%d: line %q wider than 20", h, l)
			}
		}
	}
	if got := TooSmall("Title", 0, 20, 0, 1, "x", "y"); len(got) < 3 {
		t.Fatalf("an unknown height keeps the whole message: %q", got)
	}
}

func TestTooSmallSaysWhyInDisplayCells(t *testing.T) {
	got := TooSmall("SETUP", 7, 30, 8, 0, "setup screen", "install")
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
	narrow := strings.Join(TooSmall("SETUP", 7, 20, 8, 0, "setup screen", "install"), " ")
	if !strings.Contains(narrow, "needs 24 columns, has 20") {
		t.Fatalf("narrow %q", narrow)
	}
	// A long title wraps too: every line of the screen fits.
	for _, l := range TooSmall("Let your agent consult your dots?", 40, 30, 8, 0, "setup screen", "install") {
		if ansi.StringWidth(l) > 30 {
			t.Fatalf("line %q wider than 30", l)
		}
	}
}
