package mcpinstall

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// The block setup-mcp writes into Codex's config.toml. Only these lines
// change; the rest of the file is never re-serialized.
const (
	BlockBegin = "# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)"
	BlockEnd   = "# <<< herdr-dots MCP server"
)

var (
	tableHeaderRe = regexp.MustCompile(`^\s*\[`)
	// ourHeaderRe matches [mcp_servers.herdr-dots] and its sub-tables.
	ourHeaderRe = regexp.MustCompile(`^\s*\[\s*(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\.\s*(?:herdr-dots|"herdr-dots"|'herdr-dots')\s*(?:\.[^\]]*)?\]\s*(?:#.*)?$`)
	// arrayTableRe matches [[mcp_servers.herdr-dots]], which can't be edited safely.
	arrayTableRe = regexp.MustCompile(`^\s*\[\[\s*(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\.\s*(?:herdr-dots|"herdr-dots"|'herdr-dots')\s*(?:\.[^\]]*)?\]\]`)
)

type codexServer struct {
	Command           string   `toml:"command"`
	Args              []string `toml:"args"`
	EnvVars           []string `toml:"env_vars"`
	StartupTimeoutSec int      `toml:"startup_timeout_sec"`
	ToolTimeoutSec    int      `toml:"tool_timeout_sec"`
}

// precheck rejects forms the line-based edit can't handle, before parsing
// (an array of tables doesn't decode into codexServer, which would read as
// "invalid TOML").
func precheck(s string) error {
	for _, l := range strings.Split(s, "\n") {
		if arrayTableRe.MatchString(l) {
			return fmt.Errorf("%w: [[mcp_servers.%s]] is an array of tables; remove it by hand and run setup-mcp again", ErrUnsafeEdit, ServerName)
		}
	}
	return nil
}

// eolOf is the file's line ending: CRLF when it uses any.
func eolOf(s string) string {
	if strings.Contains(s, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// checkRegion makes sure the found lines really are our table in the parsed
// file (not text inside a multi-line string or a comment) and hold nothing
// else.
func checkRegion(lines []string, start, end int, old codexConfig) error {
	if !hasServer(old) {
		return fmt.Errorf("%w: the herdr-dots markers or table at line %d are not a [mcp_servers.%s] table (inside a string?); fix it by hand", ErrUnsafeEdit, start+1, ServerName)
	}
	got, err := parseCodex(strings.Join(lines[start:end], ""))
	if err != nil || len(got.MCPServers) != 1 || !hasServer(got) {
		return fmt.Errorf("%w: lines %d-%d hold more than the herdr-dots server; fix it by hand", ErrUnsafeEdit, start+1, end)
	}
	return nil
}

type codexConfig struct {
	MCPServers map[string]codexServer `toml:"mcp_servers"`
}

func parseCodex(s string) (codexConfig, error) {
	var c codexConfig
	_, err := toml.Decode(s, &c)
	return c, err
}

// CodexBlock is the managed [mcp_servers.herdr-dots] table.
func CodexBlock(bin, configDir, stateDir string) string {
	var b strings.Builder
	b.WriteString(BlockBegin + "\n")
	b.WriteString("[mcp_servers." + ServerName + "]\n")
	fmt.Fprintf(&b, "command = %s\n", tomlString(bin))
	b.WriteString("args = " + tomlArray(ServerArgs(configDir, stateDir)) + "\n")
	b.WriteString("env_vars = " + tomlArray(ForwardEnv) + "\n")
	fmt.Fprintf(&b, "startup_timeout_sec = %d\n", CodexStartupTimeoutSec)
	fmt.Fprintf(&b, "tool_timeout_sec = %d\n", CodexToolTimeoutSec)
	b.WriteString(BlockEnd + "\n")
	return b.String()
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = tomlString(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// findRegion finds the server's lines: the managed block, else an unmarked
// [mcp_servers.herdr-dots] table with its sub-tables (without the blank and
// comment lines before the next table). end is exclusive.
func findRegion(lines []string) (start, end int, ok bool, err error) {
	for i, l := range lines {
		if strings.TrimSpace(l) != BlockBegin {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == BlockEnd {
				return i, j + 1, true, nil
			}
		}
		return 0, 0, false, fmt.Errorf("%w: the herdr-dots block has no end marker (or its begin marker sits inside a string); fix it by hand", ErrUnsafeEdit)
	}
	start = -1
	for i, l := range lines {
		if start < 0 {
			if ourHeaderRe.MatchString(l) {
				start = i
			}
			continue
		}
		if tableHeaderRe.MatchString(l) && !ourHeaderRe.MatchString(l) {
			return start, trimTail(lines, start, i), true, nil
		}
	}
	if start >= 0 {
		return start, trimTail(lines, start, len(lines)), true, nil
	}
	return 0, 0, false, nil
}

// trimTail moves end back over blank and comment lines (they introduce the
// next table, not ours).
func trimTail(lines []string, start, end int) int {
	for end > start+1 {
		t := strings.TrimSpace(lines[end-1])
		if t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		end--
	}
	return end
}

// UpsertCodexBlock returns config.toml with the managed block in place: it
// replaces the managed block or an unmarked herdr-dots table, else appends
// the block. Every other line stays byte for byte. It refuses a file that
// isn't valid TOML or defines the server in a form it can't edit safely. A
// CRLF file gets a CRLF block; a file without a final newline gets the block
// on the very next line (no blank line), which is what lets
// RemoveCodexBlock restore the original bytes.
func UpsertCodexBlock(before, bin, configDir, stateDir string) (string, error) {
	if err := precheck(before); err != nil {
		return "", err
	}
	old, err := parseCodex(before)
	if err != nil {
		return "", fmt.Errorf("%w: it is not valid TOML (%v); fix it first", ErrUnsafeEdit, err)
	}
	eol := eolOf(before)
	block := strings.ReplaceAll(CodexBlock(bin, configDir, stateDir), "\n", eol)
	lines := strings.SplitAfter(before, "\n")
	start, end, ok, err := findRegion(lines)
	if err != nil {
		return "", err
	}
	var after string
	switch {
	case ok:
		if err := checkRegion(lines, start, end, old); err != nil {
			return "", err
		}
		after = strings.Join(lines[:start], "") + block + strings.Join(lines[end:], "")
	case hasServer(old):
		return "", fmt.Errorf("%w: [mcp_servers.%s] is defined as an inline table, dotted keys or an unrecognised header; remove it by hand and run setup-mcp again", ErrUnsafeEdit, ServerName)
	default:
		after = before
		if after != "" {
			// A final newline gets a blank line before the block; an
			// unterminated last line just gets its line break.
			after += eol
		}
		after += block
	}
	got, err := parseCodex(after)
	if err != nil {
		return "", fmt.Errorf("%w: the edit would make the file invalid TOML (%v); nothing changed", ErrUnsafeEdit, err)
	}
	s := got.MCPServers[ServerName]
	if s.Command != bin || !slices.Equal(s.Args, ServerArgs(configDir, stateDir)) || !slices.Equal(s.EnvVars, ForwardEnv) ||
		s.StartupTimeoutSec != CodexStartupTimeoutSec || s.ToolTimeoutSec != CodexToolTimeoutSec {
		return "", fmt.Errorf("%w: the edited file doesn't read back as expected; nothing changed", ErrUnsafeEdit)
	}
	if err := keptOthers(old, got); err != nil {
		return "", err
	}
	return after, nil
}

// RemoveCodexBlock deletes the server's lines. After an install that
// appended the block, the result equals the original file byte for byte.
func RemoveCodexBlock(before string) (string, bool, error) {
	if err := precheck(before); err != nil {
		return "", false, err
	}
	old, err := parseCodex(before)
	if err != nil {
		return "", false, fmt.Errorf("%w: it is not valid TOML (%v); fix it first", ErrUnsafeEdit, err)
	}
	lines := strings.SplitAfter(before, "\n")
	start, end, ok, err := findRegion(lines)
	if err != nil {
		return "", false, err
	}
	if !ok {
		if hasServer(old) {
			return "", false, fmt.Errorf("%w: [mcp_servers.%s] is defined as an inline table, dotted keys or an unrecognised header; remove it by hand", ErrUnsafeEdit, ServerName)
		}
		return before, false, nil
	}
	if err := checkRegion(lines, start, end, old); err != nil {
		return "", false, err
	}
	eol := eolOf(before)
	head, tail := strings.Join(lines[:start], ""), strings.Join(lines[end:], "")
	if tail == "" && strings.TrimSpace(lines[start]) == BlockBegin {
		if strings.HasSuffix(head, eol+eol) {
			head = strings.TrimSuffix(head, eol) // the blank line the install added
		} else if strings.HasSuffix(head, eol) && !strings.HasSuffix(head, "\n"+eol) {
			head = strings.TrimSuffix(head, eol) // the line break the install added to an unterminated last line
		}
	}
	after := head + tail
	got, err := parseCodex(after)
	if err != nil {
		return "", false, fmt.Errorf("%w: the edit would make the file invalid TOML (%v); nothing changed", ErrUnsafeEdit, err)
	}
	if hasServer(got) {
		return "", false, fmt.Errorf("%w: [mcp_servers.%s] is still defined after removing the block; remove it by hand", ErrUnsafeEdit, ServerName)
	}
	if err := keptOthers(old, got); err != nil {
		return "", false, err
	}
	return after, true, nil
}

func hasServer(c codexConfig) bool {
	_, ok := c.MCPServers[ServerName]
	return ok
}

// keptOthers checks that every other MCP server survived the edit unchanged.
func keptOthers(before, after codexConfig) error {
	for name, s := range before.MCPServers {
		if name == ServerName {
			continue
		}
		if a, ok := after.MCPServers[name]; !ok || a.Command != s.Command || !slices.Equal(a.Args, s.Args) {
			return fmt.Errorf("%w: the edit would change the %q server; nothing changed", ErrUnsafeEdit, name)
		}
	}
	return nil
}
