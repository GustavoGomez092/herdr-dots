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

// ContMark starts every continuation row of a line wrapped with WrapExact
// (as in the diff viewer), so a wrapped row never passes for a line of its
// own.
const ContMark = "↪"

// WrapExact splits the display-safe line s into rows of at most w display
// cells, by grapheme cluster: the rows joined are exactly s, every space
// included (what is trusted or installed must be shown byte for byte). A
// cluster wider than w gets a row of its own.
func WrapExact(s string, w int) []string {
	w = max(1, w)
	var out []string
	var cur strings.Builder
	cw := 0
	for len(s) > 0 {
		c, cwid := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if len(c) == 0 { // never happens on valid input; stay safe
			c, cwid = s[:1], 1
		}
		s = s[len(c):]
		if cw > 0 && cw+cwid > w {
			out = append(out, cur.String())
			cur.Reset()
			cw = 0
		}
		cur.WriteString(c)
		cw += cwid
	}
	return append(out, cur.String())
}

// WrapExactWords is WrapExact that breaks after a run of spaces where it
// can (a command line keeps its arguments whole). The spaces stay at the end
// of their row, so the rows joined are still exactly s; a word wider than w
// is split by WrapExact.
func WrapExactWords(s string, w int) []string {
	w = max(1, w)
	var out []string
	cur, cw := "", 0
	for _, unit := range wordUnits(s) {
		uw := ansi.StringWidth(unit)
		if cw+uw <= w {
			cur, cw = cur+unit, cw+uw
			continue
		}
		if cur != "" {
			out = append(out, cur)
			cur, cw = "", 0
		}
		if uw > w {
			pieces := WrapExact(unit, w)
			out = append(out, pieces[:len(pieces)-1]...)
			unit = pieces[len(pieces)-1]
			uw = ansi.StringWidth(unit)
		}
		cur, cw = unit, uw
	}
	return append(out, cur)
}

// wordUnits splits s into words, each with the spaces after it (a leading
// run of spaces is a unit of its own).
func wordUnits(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || (s[i] != ' ' && s[i-1] == ' ') {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
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
			// Only an indent before a word that doesn't fit with it: the
			// indent goes, any escape sequence (a colour start) stays.
			tok = strings.ReplaceAll(cur, " ", "") + tok
			cur, curW = "", 0
		}
		if tw > w {
			pieces := strings.Split(ansi.Hardwrap(tok, w, true), "\n")
			lines = append(lines, pieces[:len(pieces)-1]...)
			tok = pieces[len(pieces)-1]
			tw = ansi.StringWidth(tok)
		}
		cur, curW = tok, tw
	}
	if cur != "" || len(lines) == 0 { // a line of spaces still shows (empty)
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
