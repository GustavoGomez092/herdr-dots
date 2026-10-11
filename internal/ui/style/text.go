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

// WrapLine breaks the single line s into lines of at most w display cells
// at spaces, keeping s's own spacing (a leading indent, aligned columns).
// The spaces at a break are dropped; a word wider than w is the only thing
// broken inside. ANSI sequences are kept and take no cells. Unlike Wrap, it
// neither cleans s nor collapses its spaces: callers pass display-safe text.
func WrapLine(s string, w int) []string {
	w = max(w, 1)
	if ansi.StringWidth(s) <= w {
		return []string{s}
	}
	var lines []string
	cur, curW := "", 0
	flush := func() {
		lines = append(lines, strings.TrimRight(cur, " "))
		cur, curW = "", 0
	}
	for _, tok := range spaceRuns(s) {
		tw := ansi.StringWidth(tok)
		if strings.TrimLeft(tok, " ") == "" { // a run of spaces
			if curW+tw <= w {
				cur, curW = cur+tok, curW+tw
			} else if cur != "" {
				flush()
			}
			continue
		}
		if curW+tw <= w {
			cur, curW = cur+tok, curW+tw
			continue
		}
		if strings.TrimSpace(ansi.Strip(cur)) != "" {
			flush()
		} else {
			cur, curW = "", 0 // only an indent before a word that doesn't fit with it
		}
		if tw > w {
			pieces := strings.Split(ansi.Hardwrap(tok, w, true), "\n")
			lines = append(lines, pieces[:len(pieces)-1]...)
			tok = pieces[len(pieces)-1]
			tw = ansi.StringWidth(tok)
		}
		cur, curW = tok, tw
	}
	if cur != "" {
		flush()
	}
	return lines
}

// spaceRuns splits s into alternating runs of spaces and of everything else.
func spaceRuns(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || (s[i] == ' ') != (s[start] == ' ') {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}

// Cut shortens s to at most w display cells, ending in Sym("…") when it cuts.
func Cut(s string, w int) string {
	return ansi.Truncate(s, w, Sym("…"))
}
