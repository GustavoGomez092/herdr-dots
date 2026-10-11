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
// height with its tail. With a known height (h > 0) the lines and the tail
// more lines the caller puts under them fit in h, so the terminal never
// drops the top (B6): the title shrinks to one cut line first, then the
// message loses its end (marked with an ellipsis). The title line and the
// "Too small" line are the last to go.
func TooSmall(title string, need, w, h, tail int, what, action string) []string {
	why := fmt.Sprintf("needs %d lines, has %d", need, h)
	if w < MinScreenWidth {
		why = fmt.Sprintf("needs %d columns, has %d", MinScreenWidth, w)
	}
	head := Wrap(title, w)
	msg := Wrap("Too small to show the whole "+what+" ("+why+"). Enlarge the popup to "+action+".", w)
	if room := h - tail; h > 0 && len(head)+len(msg) > room {
		head = []string{Cut(title, max(1, w))}
		if keep := max(1, room-1); len(msg) > keep {
			msg = msg[:keep]
			msg[keep-1] = Cut(msg[keep-1]+" "+Sym("…"), max(1, w))
		}
	}
	var lines []string
	for _, l := range head {
		lines = append(lines, Title.Render(l))
	}
	return append(lines, msg...)
}
