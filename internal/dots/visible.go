package dots

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// VisibleLines renders bytes from a file for review: every character a
// terminal would hide, reorder or act on (control characters, bidi
// controls, zero-width and other invisible format characters, invalid
// UTF-8) is shown as a visible <U+XXXX> or <0xNN> escape instead, so the
// reviewer sees every byte that is trusted. Lines end at "\n" (or "\r\n");
// tabs show as four spaces. The trust popup and `dots trust` both use it.
func VisibleLines(raw []byte) []string {
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
		case UnsafeText(string(r), false) != nil:
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
