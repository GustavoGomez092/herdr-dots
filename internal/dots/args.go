package dots

import (
	"regexp"
	"slices"
	"strings"

	"herdr-dots/internal/credentials"
)

// argRule is one backend CLI flag a dot's [backend].args may not pass, because
// it would override the read-only safety flags the backend sets.
type argRule struct {
	flag     string
	takes    bool              // the flag takes a value (inline "=v" or the next arg)
	variadic bool              // values continue until the next "-"-prefixed arg
	when     func(string) bool // deny only when the value matches; nil = always
}

var deniedArgs = map[string][]argRule{
	"claude-cli": {
		{flag: "--dangerously-skip-permissions"},
		{flag: "--allow-dangerously-skip-permissions"},
		{flag: "--bare"},
		{flag: "--permission-mode", takes: true, variadic: true},
		{flag: "--tools", takes: true, variadic: true},
		{flag: "--allowedTools", takes: true, variadic: true},
		{flag: "--allowed-tools", takes: true, variadic: true},
		{flag: "--disallowedTools", takes: true, variadic: true},
		{flag: "--disallowed-tools", takes: true, variadic: true},
		{flag: "--settings", takes: true, variadic: true},
		{flag: "--mcp-config", takes: true, variadic: true},
		{flag: "--append-system-prompt-file", takes: true, variadic: true},
		{flag: "--add-dir", takes: true, variadic: true},
		{flag: "--restricted"},
		{flag: "--setting-sources", takes: true, variadic: true},
		{flag: "-w", takes: true, variadic: true},
		{flag: "--worktree", takes: true, variadic: true},
		{flag: "--plugin-dir", takes: true, variadic: true},
		{flag: "--plugin-url", takes: true, variadic: true},
		{flag: "--agent", takes: true, variadic: true},
		{flag: "--agents", takes: true, variadic: true},
		{flag: "--resume", takes: true, variadic: true},
		{flag: "--continue"},
		{flag: "--system-prompt", takes: true, variadic: true},
		{flag: "--system-prompt-file", takes: true, variadic: true}, // P9-R4
	},
	"codex-cli": {
		{flag: "--dangerously-bypass-approvals-and-sandbox"},
		{flag: "--yolo"},
		{flag: "--full-auto"},
		{flag: "--sandbox", takes: true},
		{flag: "-s", takes: true},
		{flag: "-c", takes: true, when: codexConfigDenied},
		{flag: "--config", takes: true, when: codexConfigDenied},
		{flag: "--add-dir", takes: true, variadic: true}, // more writable roots
		{flag: "-C", takes: true},                        // another working root
		{flag: "--cd", takes: true},
		// P9-R4: another provider or profile (prompts sent elsewhere), web
		// search, feature switches, and a file written outside the sandbox.
		{flag: "-p", takes: true},
		{flag: "--profile", takes: true},
		{flag: "--oss"},
		{flag: "--local-provider", takes: true},
		{flag: "--enable", takes: true},
		{flag: "--disable", takes: true},
		{flag: "--search"},
		{flag: "-o", takes: true},
		{flag: "--output-last-message", takes: true},
	},
}

// codexConfigKeys are the only `codex -c key=value` overrides a dot may pass
// (P9-R4): every other key (base_url, model_provider(s), tools.*,
// web_search, mcp_servers, sandbox*, shell*, approval*, env, profile, ...)
// could send prompts elsewhere, turn on web access or loosen the sandbox.
var codexConfigKeys = []string{"model", "model_reasoning_effort", "model_reasoning_summary", "hide_agent_reasoning"}

func codexConfigDenied(v string) bool {
	key, _, ok := strings.Cut(v, "=")
	return !ok || !slices.Contains(codexConfigKeys, strings.TrimSpace(key))
}

// match reports whether args[i] is this rule's flag and returns the inline
// value, if any.
func (r argRule) match(a string) (inline string, hasInline, ok bool) {
	switch {
	case a == r.flag:
		return "", false, true
	case r.takes && strings.HasPrefix(a, r.flag+"="):
		return a[len(r.flag)+1:], true, true
	case r.takes && len(r.flag) == 2 && strings.HasPrefix(a, r.flag) && len(a) > 2:
		return a[2:], true, true // short flag with an attached value: -sdanger-full-access
	}
	return "", false, false
}

// scanArgs splits args into the ones safe to pass and the denied flags.
func scanArgs(kind string, args []string) (kept, denied []string) {
	rules := deniedArgs[kind]
	for i := 0; i < len(args); i++ {
		a := args[i]
		var hit *argRule
		var inline string
		var hasInline bool
		for j := range rules {
			if v, has, ok := rules[j].match(a); ok {
				hit, inline, hasInline = &rules[j], v, has
				break
			}
		}
		if hit == nil {
			kept = append(kept, a)
			continue
		}
		end := i // last index belonging to this flag
		value := inline
		if hit.takes && !hasInline {
			for end+1 < len(args) && !strings.HasPrefix(args[end+1], "-") {
				end++
				if !hit.variadic {
					break
				}
			}
			if end > i {
				value = strings.Join(args[i+1:end+1], " ")
			}
		}
		if hit.when != nil && !hit.when(value) {
			kept = append(kept, args[i:end+1]...)
		} else {
			// The flag with its value, so an error names the exact arg;
			// redacted, as errors reach the feed.
			denied = append(denied, credentials.Redact(strings.Join(args[i:end+1], " ")))
		}
		i = end
	}
	return kept, denied
}

// plainToken is a model name as an arg value: no shell or flag syntax.
var plainToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,99}$`)

// sharedArg reports whether flag with value v may stay in someone else's dot.
func sharedArg(flag, v string) bool {
	switch flag {
	case "--model", "-m":
		return plainToken.MatchString(v)
	case "--effort":
		return slices.Contains(Efforts, v)
	}
	return false
}

// sharedArgs keeps only --model/-m <model> and --effort <effort> (and the
// --flag=value forms) with plain values, for imported and project dots
// (P9-R4). Everything else is removed: an allowlist, because a flag this
// version doesn't know can't be judged.
func sharedArgs(kind string, args []string) (kept, removed []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if flag, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(flag, "--") {
			if sharedArg(flag, v) {
				kept = append(kept, a)
				continue
			}
		} else if i+1 < len(args) && sharedArg(a, args[i+1]) {
			kept = append(kept, a, args[i+1])
			i++
			continue
		}
		removed = append(removed, credentials.Redact(a))
	}
	kept = SafeArgs(kind, kept) // never needed today; the denylist still wins
	if kept == nil {
		kept = []string{}
	}
	return kept, removed
}

// DeniedArgs returns the flags in args (each with its value) that a dot of
// this backend kind may not pass, because they would override the read-only
// safety flags, send prompts elsewhere or turn on web access.
func DeniedArgs(kind string, args []string) []string {
	_, denied := scanArgs(kind, args)
	return denied
}

// SafeArgs returns args without denied flags and their values.
func SafeArgs(kind string, args []string) []string {
	kept, _ := scanArgs(kind, args)
	return kept
}
