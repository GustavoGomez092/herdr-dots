package style

import (
	"strings"

	"herdr-dots/internal/textutil"
)

// Clean makes stored or model text safe for one terminal line: control
// characters stripped and whitespace collapsed.
func Clean(s string) string {
	return strings.Join(strings.Fields(textutil.SanitizeModelText(s)), " ")
}

// Wrap breaks each paragraph of s (split on newlines, each Cleaned) into
// lines of at most w runes, on spaces where it can. Blank paragraphs stay as
// empty lines, so a purpose keeps its shape. Nothing is ever cut off.
func Wrap(s string, w int) []string {
	w = max(w, 10)
	var lines []string
	for _, para := range strings.Split(textutil.SanitizeModelText(s), "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		var cur []rune
		for _, word := range words {
			r := []rune(word)
			for len(r) > w {
				if len(cur) > 0 {
					lines, cur = append(lines, string(cur)), nil
				}
				lines, r = append(lines, string(r[:w])), r[w:]
			}
			switch {
			case len(cur) == 0:
				cur = r
			case len(cur)+1+len(r) <= w:
				cur = append(append(cur, ' '), r...)
			default:
				lines, cur = append(lines, string(cur)), r
			}
		}
		if len(cur) > 0 {
			lines = append(lines, string(cur))
		}
	}
	return lines
}
