package memory

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

type secretPattern struct {
	kind string
	re   *regexp.Regexp
}

// Checked in order; the first match names the kind.
var secretPatterns = []secretPattern{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"aws access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})`)},
	{"gitlab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}`)},
	{"slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"stripe key", regexp.MustCompile(`\b[sr]k_(?:live|test)_[A-Za-z0-9]{16,}`)},
	{"anthropic key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"openai key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_\-]{20,}`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
}

var assignRe = regexp.MustCompile(`(?i)\b(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret)\s*[:=]\s*['"]?([A-Za-z0-9_\-+/=!@#^&*.]{8,})`)

var placeholders = []string{"redacted", "example", "placeholder", "xxxx", "changeme", "your", "here", "dummy"}

var entropyTokenRe = regexp.MustCompile(`[A-Za-z0-9+/=_\-]{32,}`)

// ScanSecrets reports whether text looks like it contains a credential, and
// which kind. Model-written memory that matches is refused; user-written
// memory that matches needs a confirmation.
func ScanSecrets(text string) (bool, string) {
	for _, p := range secretPatterns {
		if p.re.MatchString(text) {
			return true, p.kind
		}
	}
	for _, m := range assignRe.FindAllStringSubmatch(text, -1) {
		if secretValue(m[1]) {
			return true, "credential assignment"
		}
	}
	for _, tok := range entropyTokenRe.FindAllString(text, -1) {
		if highEntropy(tok) {
			return true, "high-entropy string"
		}
	}
	return false, ""
}

func secretValue(v string) bool {
	low := strings.ToLower(v)
	for _, ph := range placeholders {
		if strings.Contains(low, ph) {
			return false
		}
	}
	var upper, lower, digit bool
	for _, r := range v {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	return digit || (upper && lower && len(v) >= 16)
}

// highEntropy flags long mixed-case alphanumeric runs (random keys) but not
// hex digests, UUIDs, paths or identifiers.
func highEntropy(tok string) bool {
	var up, lo, dig int
	for _, r := range tok {
		switch {
		case unicode.IsUpper(r):
			up++
		case unicode.IsLower(r):
			lo++
		case unicode.IsDigit(r):
			dig++
		}
	}
	if up < 2 || lo < 2 || dig < 2 {
		return false
	}
	return shannon(tok) >= 4.2
}

func shannon(s string) float64 {
	counts := map[rune]int{}
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}
