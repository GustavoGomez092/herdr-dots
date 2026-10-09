package manage

import (
	"fmt"
	"strings"

	"herdr-dots/internal/ui/style"
)

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
	lines := strings.Split(view, "\n")
	if f.height <= 0 || len(lines) <= f.height || fieldsEnd > len(lines) || fieldsStart >= fieldsEnd {
		return view
	}
	head, mid, tail := lines[:fieldsStart], lines[fieldsStart:fieldsEnd], lines[fieldsEnd:]
	avail := f.height - len(head) - len(tail)
	if avail < 3 {
		head = head[:min(len(head), 1)] // the title only
		keep := max(0, f.height-len(head)-3)
		if len(tail) > keep {
			tail = tail[len(tail)-keep:]
		}
		avail = f.height - len(head) - len(tail)
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
	out := append(append(append([]string(nil), head...), win...), tail...)
	return strings.Join(out, "\n")
}
