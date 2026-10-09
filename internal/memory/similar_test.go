package memory

import (
	"strings"
	"testing"
)

const (
	jwtA = "JWT stored in localStorage is readable by any XSS payload; move it to an httpOnly cookie."
	jwtB = "The JWT is stored in localStorage, which any XSS payload can read; move it to an httpOnly cookie."
)

func TestNormalizeAndFingerprint(t *testing.T) {
	if got := Normalize("  Use httpOnly-cookies, NOT localStorage!! "); got != "use httponly cookies not localstorage" {
		t.Fatalf("Normalize = %q", got)
	}
	if Fingerprint("Rate limiting added to /login.") != Fingerprint("rate LIMITING added to login") {
		t.Fatal("fingerprints must ignore case and punctuation")
	}
	if Fingerprint("a b") == Fingerprint("a c") {
		t.Fatal("different texts must differ")
	}
}

func TestSimilar(t *testing.T) {
	if s := Similar(jwtA, jwtA); s != 1 {
		t.Fatalf("identical = %v", s)
	}
	if a, b := Similar(jwtA, jwtB), Similar(jwtB, jwtA); a != b {
		t.Fatalf("not symmetric: %v vs %v", a, b)
	}
	if s := Similar(jwtA, jwtB); s < 0.7 {
		t.Fatalf("paraphrase = %v, want >= 0.7", s)
	}
	if s := Similar("Rate limiting added to /login", "Headlines use sentence case"); s != 0 {
		t.Fatalf("disjoint = %v", s)
	}
	if s := Similar("", ""); s != 0 {
		t.Fatalf("empty = %v", s)
	}
	if s := Similar("ok", "OK!"); s != 1 {
		t.Fatalf("short identical = %v", s)
	}
}

func TestFirstSentenceAndRuleSubject(t *testing.T) {
	cases := map[string]string{
		"Rate limiting added to /login.":         "Rate limiting added to /login.",
		"Passwords use MD5. Switch to argon2id.": "Passwords use MD5.",
		"Is this safe? Probably not.":            "Is this safe?",
		"first line\nsecond line":                "first line",
		"  " + strings.Repeat("a", 400):          strings.Repeat("a", 299) + "…",
	}
	for in, want := range cases {
		if got := FirstSentence(in); got != want {
			t.Errorf("FirstSentence(%q) = %q, want %q", in, got, want)
		}
	}
	if RuleSubject("CSRF omitted on /admin."+ReasonSep+"internal tool)") != "CSRF omitted on /admin." {
		t.Fatal("RuleSubject must drop the reason")
	}
}

// Review Focus 4: a long re-raise whose first sentence repeats a dismissed
// finding still matches, even though the full-text Jaccard is low.
func TestSuppressScoreCatchesLongReRaise(t *testing.T) {
	entry := "Rate limiting is missing on /login."
	msg := "Rate limiting is missing on /login. An attacker can brute-force passwords at full speed; add a token bucket per IP and per account, return 429 with Retry-After, and log lockouts so support can unlock real users."
	if full := Similar(msg, entry); full >= SuppressThreshold {
		t.Fatalf("test premise: full-text similarity %v should be low", full)
	}
	if s := SuppressScore(msg, entry+ReasonSep+"we rate-limit at the proxy)"); s < SuppressThreshold {
		t.Fatalf("SuppressScore = %v, want >= %v", s, SuppressThreshold)
	}
}
