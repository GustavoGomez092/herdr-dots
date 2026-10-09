package manage

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/ui/style"
)

// minEditorHeight is the smallest height fit lays out for.
const minEditorHeight = 3

// SetHeight sets the editor's screen height; 0 = unknown (render every line).
// With Phase 8's fields the form is taller than most popups, so View scrolls
// the field list to keep the focused field on screen.
func (f *Form) SetHeight(h int) { f.height = h }

// fit keeps the title (the first fieldsStart lines) and everything after the
// fields (cost line, error, key help) on screen and scrolls the field lines
// [fieldsStart, fieldsEnd) so lines [focusStart, focusEnd] stay visible. On a
// very small screen it sheds the title padding and the tail's leading lines
// (never the key help, the last line) before it gives up the focused field.
func (f *Form) fit(view string, fieldsStart, fieldsEnd, focusStart, focusEnd int) string {
	f.unseen = false
	lines := strings.Split(view, "\n")
	h := f.height
	if h > 0 && h < minEditorHeight {
		h = minEditorHeight // title, one field line and the key help
	}
	if h <= 0 || len(lines) <= h || fieldsEnd > len(lines) || fieldsStart >= fieldsEnd {
		return view
	}
	head, mid, tail := lines[:fieldsStart], lines[fieldsStart:fieldsEnd], lines[fieldsEnd:]
	avail := h - len(head) - len(tail)
	if avail < 3 {
		head = head[:min(len(head), 1)] // the title only
		keep := max(1, h-len(head)-3)   // the key help is the last line
		if len(tail) > keep {
			tail = tail[len(tail)-keep:]
		}
		avail = h - len(head) - len(tail)
		if avail < 1 {
			return view
		}
	}
	off := 0
	if focusStart >= fieldsStart {
		s, e := focusStart-fieldsStart, focusEnd-fieldsStart
		if avail >= 3 {
			off = max(0, min(e-avail+2, s-1)) // one line of context above
		} else {
			off = s // no room for context or arrows: show the focused line
		}
	}
	off = max(0, min(off, len(mid)-avail))
	win := append([]string(nil), mid[off:off+avail]...)
	if avail >= 3 {
		if off > 0 {
			win[0] = style.Dim.Render(fmt.Sprintf("  ↑ %d more", off+1))
		}
		if below := len(mid) - (off + avail); below > 0 {
			win[len(win)-1] = style.Dim.Render(fmt.Sprintf("  ↓ %d more", below+1))
		}
	}
	if focusStart >= fieldsStart {
		l := focusStart - fieldsStart // the focused field's label line
		drawn := l >= off && l < off+avail
		if avail >= 3 && ((off > 0 && l == off) || (len(mid)-(off+avail) > 0 && l == off+avail-1)) {
			drawn = false // an arrow replaced it
		}
		f.unseen = !drawn
	}
	out := append(append(append([]string(nil), head...), win...), tail...)
	return strings.Join(out, "\n")
}

// clipLines truncates every line to width (0 = unknown: no clipping).
func clipLines(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > width {
			lines[i] = ansi.Truncate(l, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// wrapTo wraps s to width (0 = unknown: unchanged).
func wrapTo(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Wrap(s, width, "")
}

// wrapHelp wraps the key help at its " · " separators so no hint is cut off.
func wrapHelp(help string, width int) string {
	if width <= 0 || ansi.StringWidth(help) <= width {
		return help
	}
	var lines []string
	cur := ""
	for _, part := range strings.Split(help, " · ") {
		switch {
		case cur == "":
			cur = part
		case ansi.StringWidth(cur+" · "+part) <= width:
			cur += " · " + part
		default:
			lines = append(lines, cur)
			cur = part
		}
	}
	return wrapTo(strings.Join(append(lines, cur), "\n"), width)
}
