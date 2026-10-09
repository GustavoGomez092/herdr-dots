package diff

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Long diff lines are wrapped, never cut: what the user approves at gate 2
// is what was on screen, so a payload past the right edge must show on a
// continuation row, and a line only counts as seen once every one of its
// rows has been on screen.

// contMark starts every continuation row of a wrapped diff line.
const contMark = "↪"

// minContent is the narrowest diff column in which anything is counted as
// seen or the apply question may open: below it a wide character might not
// fit a row.
const minContent = 10

// row is one screen row of the selected file's diff.
type row struct {
	src  int    // index into File.Lines
	text string // display text; continuation rows start with contMark
}

// wrapped is one file's diff wrapped to one content width.
type wrapped struct {
	file, width int
	rows        []row
	start       []int // start[i]: first row of source line i; start[len] == len(rows)
}

// wrapLine splits a sanitized line into rows of at most first cells (the
// first row) and rest cells (the others), by grapheme display width. The
// rows joined are exactly s. A cluster wider than the limit (only possible
// below minContent) gets a row of its own.
func wrapLine(s string, first, rest int) []string {
	limit := max(1, first)
	var out []string
	var cur strings.Builder
	cw := 0
	for len(s) > 0 {
		c, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if len(c) == 0 { // never happens on valid input; stay safe
			c, w = s[:1], 1
		}
		s = s[len(c):]
		if cw > 0 && cw+w > limit {
			out = append(out, cur.String())
			cur.Reset()
			cw, limit = 0, max(1, rest)
		}
		cur.WriteString(c)
		cw += w
	}
	return append(out, cur.String())
}

// wrapFile wraps every line of f to width cells, continuation rows
// included (their marker takes one cell).
func wrapFile(f File, file, width int) *wrapped {
	w := &wrapped{file: file, width: width, start: make([]int, len(f.Lines)+1)}
	for i, l := range f.Lines {
		w.start[i] = len(w.rows)
		for j, part := range wrapLine(l, width, width-ansi.StringWidth(contMark)) {
			if j > 0 {
				part = contMark + part
			}
			w.rows = append(w.rows, row{src: i, text: part})
		}
	}
	w.start[len(f.Lines)] = len(w.rows)
	return w
}

// leftWidth is the file list's width in the two-column layout.
func (m Model) leftWidth() int { return min(44, max(14, m.w()/3)) }

// contentWidth is the width of the diff column.
func (m Model) contentWidth() int {
	if m.w() < 60 {
		return m.w()
	}
	return max(1, m.w()-m.leftWidth()-3)
}

// wrap is the selected file wrapped to the current content width, from the
// cache when it matches.
func (m Model) wrap() *wrapped {
	cw := m.contentWidth()
	if m.wc != nil && m.wc.file == m.file && m.wc.width == cw {
		return m.wc
	}
	if m.file < 0 || m.file >= len(m.files) {
		return &wrapped{file: m.file, width: cw, start: []int{0}}
	}
	return wrapFile(m.files[m.file], m.file, cw)
}

// reflow caches the current wrap.
func (m *Model) reflow() { m.wc = m.wrap() }

// topLine is the source line of the first diff row shown.
func (m Model) topLine() int {
	if rows := m.wrap().rows; m.top >= 0 && m.top < len(rows) {
		return rows[m.top].src
	}
	return 0
}
