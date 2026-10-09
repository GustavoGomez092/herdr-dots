package manage

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/dots"
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
// whether the end is visible.
func (p *pager) fit(total, height int) {
	height = max(1, height)
	p.offset = max(0, min(p.offset, total-height))
	if p.offset+height >= total {
		p.seenEnd = true
	}
}

// key scrolls for j/k/down/up/space/pgdown/pgup/b and reports whether it
// handled the key.
func (p *pager) key(k string, total, height int) bool {
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
	case "G", "end":
		p.offset = total
	default:
		return false
	}
	p.fit(total, height)
	return true
}

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
// isn't). Before the first size message nothing is wrapped.
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
		out = append(out, strings.Split(ansi.Hardwrap(l, w, true), "\n")...)
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

// visibleText renders bytes from a file for review: every character a
// terminal would hide, reorder or act on (control characters, bidi
// controls, zero-width and other invisible format characters, invalid
// UTF-8) is shown as a visible <U+XXXX> or <0xNN> escape instead, so the
// reviewer sees every byte that is trusted. Lines end at "\n" (or "\r\n");
// tabs show as four spaces.
func visibleText(raw []byte) []string {
	var lines []string
	var b strings.Builder
	s := string(raw)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, "<0x%02X>", s[i])
		case r == '\n':
			lines = append(lines, b.String())
			b.Reset()
		case r == '\r' && strings.HasPrefix(s[i+size:], "\n"):
			// CRLF: TOML reads it as a newline.
		case r == '\t':
			b.WriteString("    ")
		case dots.UnsafeText(string(r), false) != nil:
			fmt.Fprintf(&b, "<U+%04X>", r)
		default:
			b.WriteRune(r)
		}
		i += size
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())
	}
	return lines
}
