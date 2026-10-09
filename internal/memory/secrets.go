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
	{"google api key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{"url credentials", regexp.MustCompile(`[a-z][a-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`)},
	{"bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
}

// assignRe matches NAME=value / "name": "value" where NAME contains a secret
// keyword, with any identifier prefix or suffix (DB_PASSWORD, SECRET_KEY).
var assignRe = regexp.MustCompile(`(?i)[A-Za-z0-9_]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)[A-Za-z0-9_]*["']?\s*[:=]\s*['"]?([A-Za-z0-9_\-+/=!@#^&*.]{8,})`)

// placeholders mark documentation values. Tradeoff: a long value (24+ runes)
// with a digit is treated as a real secret even if it contains one (the AWS
// docs key "...EXAMPLEKEY"): better to refuse a doc sample than store a key.
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
	if !(len(v) >= 24 && strings.ContainsAny(v, "0123456789")) {
		for _, ph := range placeholders {
			if strings.Contains(low, ph) {
				return false
			}
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
	// Tokens are ASCII (entropyTokenRe), so a fixed array keeps the sum order,
	// and so the float result, deterministic.
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / float64(len(s))
		h -= p * math.Log2(p)
	}
	return h
}
