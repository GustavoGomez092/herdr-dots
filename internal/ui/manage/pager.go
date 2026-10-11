package manage

import (
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/ui/style"
)

// pager scrolls a long review screen (an import or trust preview). seenEnd
// turns true once the last line has been on screen: approving needs it, so
// nothing is approved unseen.
type pager struct {
	offset  int
	seenEnd bool
}

// fit clamps the offset for total lines in a height-line window and notes
// whether the end is visible. measured is false until the popup knows its
// real size: the window is a guess then, so nothing counts as seen.
func (p *pager) fit(total, height int, measured bool) {
	height = max(1, height)
	p.offset = max(0, min(p.offset, total-height))
	if measured && p.offset+height >= total {
		p.seenEnd = true
	}
}

// key scrolls for j/k/down/up/space/pgdown/pgup/b/g/home and reports whether
// it handled the key. Every forward step overlaps the last window, so the
// end is reached only by putting every line on screen; there is no jump to
// the end on purpose (it would skip the middle).
func (p *pager) key(k string, total, height int, measured bool) bool {
	switch k {
	case "j", "down":
		p.offset++
	case "k", "up":
		p.offset--
	case " ", "pgdown", "ctrl+d":
		p.offset += max(1, height-1)
	case "pgup", "b", "ctrl+u":
		p.offset -= max(1, height-1)
	case "g", "home":
		p.offset = 0
	default:
		return false
	}
	p.fit(total, height, measured)
	return true
}

// measured reports whether a WindowSizeMsg has told the popup its size.
func (m Model) measured() bool { return m.width > 0 && m.height > 0 }

// waitingForSize is the status when y comes before the first size message.
const waitingForSize = "waiting for the window size before anything counts as read"

func (p pager) window(lines []string, height int) []string {
	height = max(1, height)
	end := min(len(lines), p.offset+height)
	return lines[min(p.offset, end):end]
}

// previewFooter is how many lines a preview keeps under its body: a status
// line, what y would do, and the key help.
const previewFooter = 4

// bodyHeight is the number of preview lines that fit above the footer.
func (m Model) bodyHeight() int {
	if m.height <= 0 {
		return 30
	}
	return max(3, m.height-previewFooter)
}

func (m Model) bodyWidth() int {
	if m.width <= 0 {
		return 80
	}
	return max(20, m.width-2)
}

// fitLines wraps every line to the popup's width once it is known, so a
// long line is never cut off the screen (and never counts as seen when it
// isn't). It breaks between words, and inside a word only when the word
// alone is wider than the line (B5). Before the first size message nothing
// is wrapped.
func (m Model) fitLines(lines []string) []string {
	if m.width <= 0 {
		return lines
	}
	w := m.bodyWidth()
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if ansi.StringWidth(l) <= w {
			out = append(out, l)
			continue
		}
		out = append(out, style.WrapLine(l, w)...)
	}
	return out
}

// cutLine shortens a single-line row (a library row, a footer) to the width.
func (m Model) cutLine(s string) string {
	if m.width <= 0 {
		return s
	}
	return style.Cut(s, max(10, m.width))
}

// libraryRow joins a library row's columns and its origin badge, cutting the
// columns first so the badge (an untrusted warning) stays on screen.
func (m Model) libraryRow(cols, badge string) string {
	if m.width <= 0 {
		return cols + badge
	}
	room := m.width - ansi.StringWidth(badge)
	if room < 10 {
		return style.Cut(cols+badge, max(10, m.width))
	}
	if ansi.StringWidth(cols) > room {
		cols = style.Cut(cols, room)
	}
	return cols + badge
}
