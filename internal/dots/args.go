package dots

import "strings"

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
	},
	"codex-cli": {
		{flag: "--dangerously-bypass-approvals-and-sandbox"},
		{flag: "--yolo"},
		{flag: "--full-auto"},
		{flag: "--sandbox", takes: true},
		{flag: "-s", takes: true},
		{flag: "-c", takes: true, when: sandboxOrMCP},
		{flag: "--config", takes: true, when: sandboxOrMCP},
	},
}

func sandboxOrMCP(v string) bool {
	return strings.Contains(v, "sandbox") || strings.Contains(v, "mcp_servers")
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
			denied = append(denied, a)
		}
		i = end
	}
	return kept, denied
}

// DeniedArgs returns the flags in args that a dot of this backend kind may not
// pass, because they would override the read-only safety flags.
func DeniedArgs(kind string, args []string) []string {
	_, denied := scanArgs(kind, args)
	return denied
}

// SafeArgs returns args without denied flags and their values.
func SafeArgs(kind string, args []string) []string {
	kept, _ := scanArgs(kind, args)
	return kept
}
