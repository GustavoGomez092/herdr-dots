// Package style holds the colors and text styles shared by the Dots TUIs.
package style

import "github.com/charmbracelet/lipgloss"

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
	return lipgloss.NewStyle().Foreground(Color(color)).Render(glyph)
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
