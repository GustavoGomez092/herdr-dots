package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SuppressThreshold is the SuppressScore at or above which a new verdict is
// treated as a repeat of a remembered one (spec M10, ruling 13).
const SuppressThreshold = 0.7

// ReasonSep separates a rule's subject from the developer's reason.
const ReasonSep = " (reason: "

var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the and for with this that these those are was were been being not
	you your our ours from into onto has have had any can could should would will which who whom what
	when where there their them they then than also but all its use using used via per out off now yet
	too very just here some such only own same each both more most other about over under does did
	doing don doesn didn isn aren wasn weren won`) {
		stopwords[w] = true
	}
}

// Normalize lowercases s and turns every run of non-letters/digits into one space.
func Normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// Fingerprint is a short hash of Normalize(s): equal for texts that differ
// only in case, spacing or punctuation.
func Fingerprint(s string) string {
	sum := sha256.Sum256([]byte(Normalize(s)))
	return hex.EncodeToString(sum[:8])
}

// tokens are the normalized words of 3+ runes that aren't stopwords.
func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(Normalize(s)) {
		if utf8.RuneCountInString(w) >= 3 && !stopwords[w] {
			out[w] = true
		}
	}
	return out
}

// Similar is the Jaccard similarity of the two texts' tokens (0..1).
func Similar(a, b string) float64 {
	ta, tb := tokens(a), tokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		if na := Normalize(a); na != "" && na == Normalize(b) {
			return 1
		}
		return 0
	}
	inter := 0
	for w := range ta {
		if tb[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(ta)+len(tb)-inter)
}

// FirstSentence returns s up to the first ". ", "! ", "? " or newline, at most
// 300 runes.
func FirstSentence(s string) string {
	s = strings.TrimSpace(s)
	cut := len(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		cut = i
	}
	for _, sep := range []string{". ", "! ", "? "} {
		if i := strings.Index(s, sep); i >= 0 && i+1 < cut {
			cut = i + 1
		}
	}
	return clipRunes(strings.TrimSpace(s[:cut]), 300)
}

// RuleSubject drops the developer's " (reason: …)" suffix from a rule.
func RuleSubject(text string) string {
	subj, _, _ := strings.Cut(text, ReasonSep)
	return subj
}

// SuppressScore compares a new verdict message with a remembered entry: both
// the full message and its first sentence are compared with the entry's
// subject, and the higher score wins (a long re-raise still matches).
func SuppressScore(msg, entryText string) float64 {
	subj := RuleSubject(entryText)
	return max(Similar(msg, subj), Similar(FirstSentence(msg), subj))
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
