package style

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// MinScreenWidth is the narrowest popup a whole confirm screen (memui's
// approve screen, the Setup screen) is laid out in.
const MinScreenWidth = 24

// ScreenFits reports whether a whole-popup confirm screen of lines, with tail
// more lines under it (a prompt, a footer), is on screen to the last cell in
// a w×h popup: what the developer confirms must be shown whole (Task 10). A
// taller screen has its top lines cut by the terminal and a wider line loses
// its right end. An unknown height (h <= 0, no WindowSizeMsg yet) fits.
func ScreenFits(lines []string, w, h, tail int) bool {
	if w < MinScreenWidth {
		return false
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > w {
			return false
		}
	}
	return h <= 0 || len(lines)+tail <= h
}

// TooSmall is what a popup shows instead of a confirm screen that doesn't
// fit: its title and why, wrapped by display cells. need is the screen's
// height with its tail.
func TooSmall(title string, need, w, h int, what, action string) []string {
	why := fmt.Sprintf("needs %d lines, has %d", need, h)
	if w < MinScreenWidth {
		why = fmt.Sprintf("needs %d columns, has %d", MinScreenWidth, w)
	}
	var lines []string
	for _, l := range Wrap(title, w) {
		lines = append(lines, Title.Render(l))
	}
	return append(lines, Wrap("Too small to show the whole "+what+" ("+why+"). Enlarge the popup to "+action+".", w)...)
}
