// Package mcpinstall installs and removes the Dots MCP server in Claude Code
// and Codex (`herdr-dots setup-mcp`). It computes a Plan that the command
// previews first; Apply backs up every file it touches before changing it,
// and refuses when a file changed after the preview.
package mcpinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"herdr-dots/internal/fsutil"
)

type Client string

const (
	ClientClaude Client = "claude"
	ClientCodex  Client = "codex"
)

// Title is the client's display name.
func (c Client) Title() string {
	if c == ClientCodex {
		return "Codex"
	}
	return "Claude Code"
}

// ServerName is the MCP server's name in both clients; tools show up as
// mcp__herdr-dots__<tool>.
const ServerName = "herdr-dots"

// ForwardEnv are the variables the server needs from the agent's pane.
var ForwardEnv = []string{"HERDR_PANE_ID", "HERDR_SOCKET_PATH"}

// Codex timeouts written into [mcp_servers.herdr-dots].
const (
	CodexStartupTimeoutSec = 20
	CodexToolTimeoutSec    = 180
)

// Env locates the clients' config files (tests point it at temp dirs).
type Env struct {
	Getenv func(string) string
	Home   string
}

func (e Env) get(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// ClaudeConfigPath is the file `claude mcp add --scope user` edits.
func (e Env) ClaudeConfigPath() string {
	if d := e.get("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	return filepath.Join(e.Home, ".claude.json")
}

// CodexConfigPath is Codex's user config.
func (e Env) CodexConfigPath() string {
	if d := e.get("CODEX_HOME"); d != "" {
		return filepath.Join(d, "config.toml")
	}
	return filepath.Join(e.Home, ".codex", "config.toml")
}

// Command is one CLI call; a MayFail command's failure is ignored (and its
// output hidden).
type Command struct {
	Args    []string
	MayFail bool
}

// FileEdit replaces a whole file: Before is what the preview showed.
type FileEdit struct {
	Path, Before, After string
}

// Plan is what setup-mcp shows and then applies for one client.
type Plan struct {
	Client   Client
	Remove   bool
	Commands []Command
	Edits    []FileEdit
	Backups  []string // copied aside before anything changes
	Verify   []string // shows the result afterwards
	Preview  string   // the change, as a diff or the expected entry
	Nothing  bool     // nothing to do
	Config   string   // the client's config file
}

// ErrUnsafeEdit: the config can't be edited without risking the user's file.
var ErrUnsafeEdit = errors.New("refusing to edit")

// ServerArgs are the server's arguments after the binary.
func ServerArgs(configDir, stateDir string) []string {
	return []string{"mcp", "--config-dir", configDir, "--state-dir", stateDir}
}

func checkInputs(bin, configDir, stateDir string) error {
	for _, p := range []string{bin, configDir, stateDir} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%q is not an absolute path", p)
		}
		if strings.IndexFunc(p, unicode.IsControl) >= 0 {
			return fmt.Errorf("%q contains control characters", p)
		}
	}
	return nil
}

// PlanInstall plans installing the server for c (user scope).
func PlanInstall(c Client, env Env, bin, configDir, stateDir string) (Plan, error) {
	if err := checkInputs(bin, configDir, stateDir); err != nil {
		return Plan{}, err
	}
	switch c {
	case ClientClaude:
		return planClaudeInstall(env, bin, configDir, stateDir), nil
	case ClientCodex:
		return planCodexInstall(env, bin, configDir, stateDir)
	}
	return Plan{}, fmt.Errorf("unknown client %q (want claude or codex)", c)
}

// PlanRemove plans removing the server from c.
func PlanRemove(c Client, env Env) (Plan, error) {
	switch c {
	case ClientClaude:
		p := Plan{Client: c, Remove: true, Config: env.ClaudeConfigPath(),
			Commands: []Command{{Args: claudeRemoveArgs()}},
			Verify:   []string{"claude", "mcp", "list"},
			Preview:  "Claude Code removes the \"" + ServerName + "\" entry from " + env.ClaudeConfigPath() + ".\n"}
		if _, found := readClaudeEntry(p.Config); !found {
			// `claude mcp remove` would exit 1 ("No MCP server named …").
			return Plan{Client: c, Remove: true, Config: p.Config, Nothing: true,
				Preview: "not installed in " + p.Config + "\n"}, nil
		}
		if exists(p.Config) {
			p.Backups = []string{p.Config}
		}
		return p, nil
	case ClientCodex:
		path := env.CodexConfigPath()
		if err := checkNotDangling(path); err != nil {
			return Plan{}, err
		}
		before, err := readOptional(path)
		if err != nil {
			return Plan{}, err
		}
		after, found, err := RemoveCodexBlock(before)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", path, err)
		}
		p := Plan{Client: c, Remove: true, Config: path, Verify: []string{"codex", "mcp", "list"}}
		if !found {
			p.Nothing, p.Preview = true, "not installed in "+path+"\n"
			return p, nil
		}
		p.Edits = []FileEdit{{Path: path, Before: before, After: after}}
		p.Backups = []string{path}
		p.Preview = Diff(before, after)
		return p, nil
	}
	return Plan{}, fmt.Errorf("unknown client %q (want claude or codex)", c)
}

func claudeRemoveArgs() []string {
	return []string{"claude", "mcp", "remove", "--scope", "user", ServerName}
}

// planClaudeInstall removes any older entry (allowed to fail) and adds the
// server with the pane variables forwarded through ${VAR:-} expansion, so a
// Claude Code started outside Herdr hands the server empty values.
func planClaudeInstall(env Env, bin, configDir, stateDir string) Plan {
	add := []string{"claude", "mcp", "add", "--scope", "user"}
	for _, k := range ForwardEnv {
		add = append(add, "--env", k+"=${"+k+":-}")
	}
	add = append(add, "--transport", "stdio", ServerName, "--", bin)
	add = append(add, ServerArgs(configDir, stateDir)...)
	p := Plan{Client: ClientClaude, Config: env.ClaudeConfigPath(),
		Commands: []Command{{Args: claudeRemoveArgs(), MayFail: true}, {Args: add}},
		Verify:   []string{"claude", "mcp", "get", ServerName}}
	if e, found := readClaudeEntry(p.Config); found && claudeEntryMatches(e, bin, configDir, stateDir) {
		// Already as planned: no commands and no backup of a multi-MB file.
		return Plan{Client: ClientClaude, Config: p.Config, Nothing: true, Verify: p.Verify,
			Preview: "already installed in " + p.Config + "\n"}
	}
	if exists(p.Config) {
		p.Backups = []string{p.Config}
	}
	entry := map[string]any{"type": "stdio", "command": bin, "args": ServerArgs(configDir, stateDir), "env": claudeEnv()}
	b, _ := json.MarshalIndent(map[string]any{ServerName: entry}, "", "  ")
	var pv strings.Builder
	fmt.Fprintf(&pv, "Claude Code stores this under \"mcpServers\" in %s (user scope):\n", p.Config)
	for _, l := range strings.Split(string(b), "\n") {
		pv.WriteString("+ " + l + "\n")
	}
	p.Preview = pv.String()
	return p
}

func claudeEnv() map[string]string {
	m := map[string]string{}
	for _, k := range ForwardEnv {
		m[k] = "${" + k + ":-}"
	}
	return m
}

// claudeEntry is the part of an mcpServers entry the installer compares.
type claudeEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// readClaudeEntry reads (never writes) the user-scope entry from Claude's
// config. found is true when the entry exists, and also when the file can't
// be parsed: then the claude CLI decides.
func readClaudeEntry(path string) (claudeEntry, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return claudeEntry{}, false
	}
	var f struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &f) != nil {
		return claudeEntry{}, true
	}
	raw, ok := f.MCPServers[ServerName]
	if !ok {
		return claudeEntry{}, false
	}
	var e claudeEntry
	_ = json.Unmarshal(raw, &e)
	return e, true
}

func claudeEntryMatches(e claudeEntry, bin, configDir, stateDir string) bool {
	return (e.Type == "stdio" || e.Type == "") && e.Command == bin &&
		slices.Equal(e.Args, ServerArgs(configDir, stateDir)) && maps.Equal(e.Env, claudeEnv())
}

// checkNotDangling refuses a symlink whose target is missing: writing would
// replace the link with a regular file.
func checkNotDangling(path string) error {
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s is a dangling symlink; fix the link first, nothing changed", path)
	}
	return nil
}

func planCodexInstall(env Env, bin, configDir, stateDir string) (Plan, error) {
	path := env.CodexConfigPath()
	if err := checkNotDangling(path); err != nil {
		return Plan{}, err
	}
	before, err := readOptional(path)
	if err != nil {
		return Plan{}, err
	}
	after, err := UpsertCodexBlock(before, bin, configDir, stateDir)
	if err != nil {
		return Plan{}, fmt.Errorf("%s: %w", path, err)
	}
	p := Plan{Client: ClientCodex, Config: path, Verify: []string{"codex", "mcp", "list"}}
	if before == after {
		p.Nothing, p.Preview = true, "already installed in "+path+"\n"
		return p, nil
	}
	p.Edits = []FileEdit{{Path: path, Before: before, After: after}}
	if exists(path) {
		p.Backups = []string{path}
	}
	p.Preview = Diff(before, after)
	return p, nil
}

// Apply carries out a plan: it checks that no file changed since the preview,
// backs up, writes the edits, then runs the commands. run gets each command;
// it returns the command's error.
func Apply(p Plan, run func(Command) error, now time.Time) ([]string, error) {
	if p.Nothing {
		return nil, nil
	}
	for _, e := range p.Edits {
		cur, err := readOptional(e.Path)
		if err != nil {
			return nil, err
		}
		if cur != e.Before {
			return nil, fmt.Errorf("%s changed since the preview; nothing was changed: run setup-mcp again", e.Path)
		}
	}
	var backups []string
	for _, f := range p.Backups {
		b, err := backup(f, now)
		if err != nil {
			return backups, fmt.Errorf("back up %s: %w", f, err)
		}
		backups = append(backups, b)
	}
	for _, e := range p.Edits {
		if err := writeKeepingMode(e.Path, e.After); err != nil {
			return backups, err
		}
	}
	for _, c := range p.Commands {
		if err := run(c); err != nil && !c.MayFail {
			return backups, fmt.Errorf("`%s` failed: %w", strings.Join(c.Args, " "), err)
		}
	}
	return backups, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readOptional reads a file; a missing file reads as "".
func readOptional(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// backup copies path to path.bak-herdr-dots-<time> (0600), never overwriting
// an older backup.
func backup(path string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	base := path + ".bak-herdr-dots-" + now.Format("20060102-150405")
	for i := 0; i < 100; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		return name, f.Close()
	}
	return "", fmt.Errorf("too many backups of %s", path)
}

// writeKeepingMode writes atomically through a dotfiles symlink, keeping the
// file's mode (0600 for a new file).
func writeKeepingMode(path, content string) error {
	if err := checkNotDangling(path); err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return fsutil.WriteFileAtomic(path, []byte(content), mode)
}
