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
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{6,}`), "sk-ant-" + Redacted},
	{regexp.MustCompile(`sk-or-[A-Za-z0-9_\-]{6,}`), "sk-or-" + Redacted},
	{regexp.MustCompile(`sk-[A-Za-z0-9_\-]{12,}`), "sk-" + Redacted},
	// Authorization: Bearer <token>
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=\-]{8,}`), "${1}" + Redacted},
	// api_key=…, "api_key": "…", x-api-key: …, authorization: …
	{regexp.MustCompile(`(?i)((?:api[_-]?key|x-api-key|authorization|access[_-]?token)["']?\s*[:=]\s*["']?)[^\s"'&,;}]{6,}`), "${1}" + Redacted},
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
