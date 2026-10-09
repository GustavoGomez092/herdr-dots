// Package textutil holds small text helpers shared across packages.
package textutil

import (
	"strings"
	"unicode"
)

// SanitizeModelText removes control characters (C0, DEL, C1: ESC, BEL, CSI,
// OSC...) and bidirectional formatting controls (which can reorder what a
// terminal shows) from text that came from a model, keeping newlines and
// tabs. Model output reaches terminals (the Dots pane) and agent prompts,
// where escape sequences could rewrite the screen or the clipboard. The zero
// width joiner and variation selectors stay so emoji still render.
func SanitizeModelText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r), isBidi(r):
			return -1
		}
		return r
	}, s)
}

// isBidi reports the bidi embedding, override, isolate and mark controls.
func isBidi(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x200E || r == 0x200F || r == 0x061C
}
