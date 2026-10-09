package style

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/textutil"
)

// Clean makes stored or model text safe for one terminal line: control
// characters stripped and whitespace collapsed.
func Clean(s string) string {
	return strings.Join(strings.Fields(textutil.SanitizeModelText(s)), " ")
}

// Wrap breaks each paragraph of s (split on newlines, each Cleaned) into
// lines of at most w display cells, on spaces where it can. Blank paragraphs
// stay as empty lines, so a purpose keeps its shape. Nothing is ever cut off.
func Wrap(s string, w int) []string {
	w = max(w, 10)
	var lines []string
	for _, para := range strings.Split(textutil.SanitizeModelText(s), "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur, curW := "", 0
		for _, word := range words {
			ww := ansi.StringWidth(word)
			if ww > w {
				if cur != "" {
					lines, cur, curW = append(lines, cur), "", 0
				}
				pieces := strings.Split(ansi.Hardwrap(word, w, true), "\n")
				lines = append(lines, pieces[:len(pieces)-1]...)
				word = pieces[len(pieces)-1]
				ww = ansi.StringWidth(word)
			}
			switch {
			case cur == "":
				cur, curW = word, ww
			case curW+1+ww <= w:
				cur, curW = cur+" "+word, curW+1+ww
			default:
				lines = append(lines, cur)
				cur, curW = word, ww
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	return lines
}

// Cut shortens s to at most w display cells, ending in Sym("…") when it cuts.
func Cut(s string, w int) string {
	return ansi.Truncate(s, w, Sym("…"))
}
