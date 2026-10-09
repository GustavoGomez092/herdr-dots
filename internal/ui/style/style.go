// Package style holds the colors and text styles shared by the Dots TUIs.
package style

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

var palette = map[string]lipgloss.Color{
	"red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
}

func Color(name string) lipgloss.Color {
	if c, ok := palette[name]; ok {
		return c
	}
	return "7"
}

var (
	Title    = lipgloss.NewStyle().Bold(true)
	Dim      = lipgloss.NewStyle().Faint(true)
	Selected = lipgloss.NewStyle().Reverse(true)
	Err      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func Glyph(glyph, color string) string {
	return lipgloss.NewStyle().Foreground(Color(color)).Render(Sym(glyph))
}

// ascii is on when HERDR_DOTS_ASCII=1: for terminals or fonts that show the
// dot glyphs as empty boxes (some Windows console fonts).
var ascii = os.Getenv("HERDR_DOTS_ASCII") == "1"

// ASCII reports whether symbols are drawn as ASCII stand-ins.
func ASCII() bool { return ascii }

// asciiSyms are the stand-ins for the symbols the Dots UIs and the dot
// templates use.
var asciiSyms = map[string]string{
	"◆": "*", "▲": "^", "✎": "~", "⊕": "+", "◌": "o", "★": "*", "▼": "v", "✓": "+", "…": "...", "☾": "(",
}

// Sym returns s unchanged, or with ASCII() on its ASCII stand-in: a known
// symbol's mapping, s itself when it is already ASCII, else "*".
func Sym(s string) string {
	if !ascii {
		return s
	}
	if a, ok := asciiSyms[s]; ok {
		return a
	}
	if strings.IndexFunc(s, func(r rune) bool { return r >= utf8.RuneSelf }) < 0 {
		return s
	}
	return "*"
}

func Severity(sev string) lipgloss.Style {
	switch sev {
	case "critical":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	case "warn":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
}
