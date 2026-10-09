// Package textutil holds small text helpers shared across packages.
package textutil

import (
	"strings"
	"unicode"
)

// SanitizeModelText removes control characters (C0, DEL, C1: ESC, BEL, CSI,
// OSC...) from text that came from a model, keeping newlines and tabs. Model
// output reaches terminals (the Dots pane) and agent prompts, where escape
// sequences could rewrite the screen or the clipboard.
func SanitizeModelText(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
