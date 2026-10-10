package credentials

import (
	"regexp"
	"strings"
)

// Redacted replaces every secret Redact finds.
const Redacted = "[redacted]"

var redactions = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Provider key shapes. sk-ant- and sk-or- come before the generic sk-.
	{regexp.MustCompile(`(^|[^A-Za-z])sk-ant-[A-Za-z0-9_\-]{6,}`), "${1}sk-ant-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z])sk-or-[A-Za-z0-9_\-]{6,}`), "${1}sk-or-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z])sk-[A-Za-z0-9_\-]{12,}`), "${1}sk-" + Redacted},
	// Shapes internal/memory.ScanSecrets detects (kept in step by a test there).
	{regexp.MustCompile(`(^|[^A-Za-z0-9])(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})`), "${1}gh-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z0-9])(?:AKIA|ASIA)[0-9A-Z]{16}\b`), "${1}aws-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z0-9])xox[abprs]-[A-Za-z0-9-]{10,}`), "${1}xox-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z0-9])[sr]k_(?:live|test)_[A-Za-z0-9]{16,}`), "${1}stripe-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z0-9])glpat-[A-Za-z0-9_\-]{20,}`), "${1}glpat-" + Redacted},
	{regexp.MustCompile(`(^|[^A-Za-z0-9])([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`), "${1}${2}" + Redacted + "@"},
	// Authorization: Bearer <token>
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=\-]{16,}`), "${1}" + Redacted},
	// api_key=…, "api_key": "…", x-api-key: …, authorization: …
	{regexp.MustCompile(`(?i)((?:api[_-]?key|x-api-key|access[_-]?token)["']?\s*[:=]\s*["']?)[^\s"'&,;}]{6,}`), "${1}" + Redacted},
	// A query-string credential by parameter name (S13): ?key=…, &token=…
	{regexp.MustCompile(`(?i)([?&](?:key|token|auth|secret|client_secret|password|passwd|sig|signature)=)[^\s"'&#]{6,}`), "${1}" + Redacted},
	// authorization: [scheme] credential (Basic, token, Bearer, ...)
	{regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?)(?:\w+\s+)?[^\s"'&,;}]{6,}`), "${1}" + Redacted},
}

// Redact scrubs secrets from s before it reaches a log, the feed, a toast, a
// snapshot or an error string: first the exact known keys (len >= 8), then
// anything shaped like a provider key, a bearer token or an api_key value.
func Redact(s string, known ...string) string {
	for _, k := range known {
		if len(k) >= 8 {
			s = strings.ReplaceAll(s, k, Redacted)
		}
	}
	for _, r := range redactions {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
