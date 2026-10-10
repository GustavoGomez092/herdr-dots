package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/platform"
)

// A claude draft run gets the tools in ClaudeWriteTools. Only the read
// tools are pre-approved (ClaudeWriteAllowed): a bare Edit or Write allow
// rule approves edits to any path, while acceptEdits approves edits inside
// the working directory only (verified live, Phase 7 V1). ClaudeWriteDenied
// are refused even if a setting would allow them.
const (
	ClaudeWriteTools   = "Read,Grep,Glob,Edit,Write"
	ClaudeWriteAllowed = "Read,Grep,Glob"
	ClaudeWriteDenied  = "Bash,WebFetch,WebSearch"
)

// writeSettings is the draft run's --settings: hooks off, and the worktree's
// .git file and any .claude/ directory (settings, hooks) are not editable
// even under acceptEdits.
const writeSettings = `{"disableAllHooks":true,"permissions":{"deny":["Edit(.git)","Write(.git)","Edit(.git/**)","Write(.git/**)","Edit(.claude/**)","Write(.claude/**)","Edit(**/.git)","Write(**/.git)","Edit(**/.claude/**)","Write(**/.claude/**)"]}}`

type ClaudeCLI struct {
	Runner    Runner
	Bin       string          // default "claude"
	Environ   func() []string // default os.Environ
	Deny      []string        // extra deny globs (config tool_deny_globs) for research runs
	ConfigDir string          // the herdr-dots config dir (holds credentials.toml): Read is denied there
}

func (c ClaudeCLI) Kind() string { return "claude-cli" }

func (c ClaudeCLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "claude"
}

func (c ClaudeCLI) environ() []string {
	if c.Environ != nil {
		return c.Environ()
	}
	return os.Environ()
}

func (c ClaudeCLI) Check(context.Context) error {
	if _, err := platform.LookPath(c.bin()); err != nil {
		return fmt.Errorf("claude CLI not found on PATH: install Claude Code (https://claude.com/claude-code)")
	}
	return nil
}

// hooklessSettings disables every hook for a dot run.
const hooklessSettings = `{"disableAllHooks":true}`

// researchSettings is the --settings of a research pass and of every other
// read-only run that has the read tools (security review R3): hooks off, and Read
// denied for every secret file the API tools deny (toolloop.DefaultDeny),
// the agent configuration drafts may not touch (toolloop.DraftDeny),
// credential files under any name, and the developer's extra globs (config
// tool_deny_globs, ruling P10-R7). Each glob is denied at the root and at
// any depth, like toolloop.Denied. Paths outside the repository are denied
// too (post-rc hardening, R3): the developer's home secrets (homeDeny,
// Claude Code's `~/` rules) and configDir, the herdr-dots config directory
// that holds credentials.toml, as an absolute `//` rule ("" adds none).
func researchSettings(extra []string, configDir string) string {
	globs := append(append([]string(nil), toolloop.DefaultDeny...), toolloop.DraftDeny...)
	globs = append(globs, "credentials*")
	globs = append(globs, extra...)
	var deny []string
	seen := map[string]bool{}
	add := func(g string) {
		if r := "Read(" + g + ")"; !seen[r] {
			seen[r] = true
			deny = append(deny, r)
		}
	}
	for _, g := range globs {
		add(g)
		if !strings.HasPrefix(g, "**/") {
			add("**/" + g)
		}
	}
	for _, g := range homeDeny {
		add(g)
	}
	if configDir != "" {
		add(absRule(configDir))
		if real, err := filepath.EvalSymlinks(configDir); err == nil {
			add(absRule(real)) // macOS: /var is /private/var
		}
	}
	type perms struct {
		Deny []string `json:"deny"`
	}
	b, err := json.Marshal(struct {
		DisableAllHooks bool  `json:"disableAllHooks"`
		Permissions     perms `json:"permissions"`
	}{true, perms{deny}})
	if err != nil {
		panic(err) // strings only: never fails
	}
	return string(b)
}

// homeDeny are the developer's home secrets no read tool may open, whatever
// the run's working directory (Claude Code expands `~/` to the home dir).
var homeDeny = []string{
	"~/.ssh/**", "~/.aws/**", "~/.gnupg/**", "~/.config/gh/**", "~/.netrc",
	"~/.docker/config.json", "~/.kube/**", "~/.claude.json", "~/.claude/**", "~/.codex/**",
}

// absRule is the glob for everything under dir, an absolute path. Claude
// Code reads a rule path that starts with `//` from the filesystem root and
// matches a Windows drive path in its POSIX form (C:\x is /c/x).
func absRule(dir string) string {
	p := filepath.ToSlash(filepath.Clean(dir))
	if vol := filepath.VolumeName(dir); len(vol) == 2 && vol[1] == ':' {
		p = "/" + strings.ToLower(vol[:1]) + p[2:]
	}
	return "/" + strings.TrimSuffix(p, "/") + "/**"
}

// BuildCmd: print mode, JSON output, no saved session, no MCP servers, hooks
// disabled (so Herdr's integration hooks never fire for a dot), read-only tools.
//
// The system prompt rides in argv up to platform.MaxInlinePrompt characters.
// A longer one, or any prompt when claude resolves to a .cmd/.bat shim
// (cmd.exe can't carry its quotes, % signs and newlines), goes in a private
// temp file passed with --append-system-prompt-file; for a shim the settings
// JSON goes in a file too. Cmd.Cleanup deletes the files: call Cmd.Close
// after the run. If the files can't be written, the v1 argv is used.
func (c ClaudeCLI) BuildCmd(r Request) Cmd {
	sys := SystemPrompt(r)
	settings := hooklessSettings
	research := r.Pass == PassResearch && !WriteMode(r)
	switch {
	case WriteMode(r):
		settings = writeSettings
	case research, r.RepoRead:
		// Read deny rules for secret files on every run that has the read
		// tools (security review R3), not only research.
		settings = researchSettings(c.Deny, c.ConfigDir)
	}
	settingsArg, promptFlag, promptArg := settings, "--append-system-prompt", sys
	shim := false
	if p, err := resolveBin(c.bin()); err == nil {
		shim = platform.IsBatchFile(p)
	}
	var cleanup func()
	if shim || utf8.RuneCountInString(sys) > platform.MaxInlinePrompt {
		if files, done, err := writeRunFiles(map[string]string{"system.txt": sys, "settings.json": settings}); err == nil {
			cleanup = done
			promptFlag, promptArg = "--append-system-prompt-file", files["system.txt"]
			if shim {
				settingsArg = files["settings.json"]
			}
		}
	}
	args := []string{
		"-p", "--output-format", "json",
		"--no-session-persistence",
		"--strict-mcp-config",
		"--settings", settingsArg,
		promptFlag, promptArg,
	}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	switch {
	case WriteMode(r):
		// A draft: file tools only; acceptEdits approves edits inside the
		// worktree (print mode can't ask, so edits elsewhere are denied); no
		// shell, no web. Never bypassPermissions.
		// --restricted ignores user/project/local settings files, so a repo's
		// .claude/settings.json can't add allow rules or directories; it keeps
		// --settings, confines file tools to the working dir and refuses
		// bypassPermissions. Verified live on Claude Code 2.1.295 (subscription
		// auth): with a repo allow-Edit/Write rule, a Write to the parent dir
		// was denied and an inside edit worked; with writeSettings, Write to
		// .git and .claude/settings.local.json was denied in a linked worktree.
		args = append(args, "--restricted")
		args = append(args, "--tools", ClaudeWriteTools, "--allowedTools", ClaudeWriteAllowed,
			"--permission-mode", "acceptEdits", "--disallowedTools", ClaudeWriteDenied)
	case research:
		// Idle research (Phase 8): no Bash, Edit or Write, and with the web
		// only WebSearch, never WebFetch, so an unattended run can't fetch a
		// URL that carries repository data out (P8-R14). --restricted keeps user/project settings
		// from adding allow rules or directories (as for drafts) and keeps
		// researchSettings, whose deny rules keep secret files out of the
		// read tools.
		args = append(args, "--restricted")
		var tools []string
		if r.RepoRead {
			tools = append(tools, "Read", "Grep", "Glob")
		}
		if r.Web {
			tools = append(tools, "WebSearch")
		}
		list := strings.Join(tools, ",")
		if list == "" {
			args = append(args, "--tools", "")
		} else {
			args = append(args, "--tools", list, "--allowedTools", list)
		}
	case r.RepoRead:
		args = append(args, "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob")
	default:
		args = append(args, "--tools", "")
	}
	if !WriteMode(r) && !research {
		// Print mode skips the workspace trust dialog, so claude would load
		// the repository's .claude/settings.json and settings.local.json:
		// commands it runs (apiKeyHelper, awsAuthRefresh, ...), env such as
		// ANTHROPIC_BASE_URL, plugins. A cloned repository is untrusted, so
		// only the developer's own user settings load (security review R1).
		// Drafts and research run --restricted, which loads none of them.
		// Before the dot's args, like the budget cap below.
		args = append(args, "--setting-sources", "user")
	}
	// Denied args (dots.Validate rejects them too) are dropped so they can never
	// override the flags above.
	dotArgs := dots.SafeArgs(c.Kind(), r.Args)
	if r.MaxBudgetUSD > 0 {
		// A background run's dollar cap (P8-R15). The dot's own caps are
		// stripped so they can't raise it, and the cap goes before the dot's
		// args: commander gives a value-taking flag the next argument even
		// when it starts with "-", so a dangling "--model" at the end of the
		// dot's args would otherwise swallow the cap.
		dotArgs = stripBudgetArgs(dotArgs)
		args = append(args, "--max-budget-usd", strconv.FormatFloat(r.MaxBudgetUSD, 'f', -1, 64))
	}
	args = append(args, dotArgs...)
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: UserPrompt(r), Cleanup: cleanup}
}

func (c ClaudeCLI) Run(ctx context.Context, r Request) (Result, error) {
	if err := checkArgs(c.Kind(), r.Args); err != nil {
		return Result{}, err
	}
	if err := checkResearchNotWrite(r); err != nil {
		return Result{}, err
	}
	if c.Runner == nil {
		return Result{}, ErrNoRunner
	}
	cmd := c.BuildCmd(r)
	defer cmd.Close()
	out, stderr, err := c.Runner.Run(ctx, cmd)
	if Preflight(err) {
		return Result{}, fmt.Errorf("claude: %w", err)
	}
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("claude timed out: %w", ctx.Err())
	}
	res, perr := parseClaude(out, r.Pass)
	if err != nil && perr != nil {
		// res carries only the usage and cost an is_error reply reported
		// (zero otherwise), so the failed run is still charged.
		return res, fmt.Errorf("claude failed: %s", Hint(c.Kind(), string(stderr)+"\n"+string(out)))
	}
	return res, perr
}

// ParseClaudeOutput parses a review run's `claude -p --output-format json` output.
func ParseClaudeOutput(out []byte) (Result, error) { return parseClaude(out, PassReview) }

func parseClaude(out []byte, pass string) (Result, error) {
	var o struct {
		IsError bool     `json:"is_error"`
		Result  string   `json:"result"`
		Cost    *float64 `json:"total_cost_usd"`
		Usage   struct {
			In         int `json:"input_tokens"` // uncached input only
			Out        int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &o); err != nil {
		return Result{}, fmt.Errorf("claude output is not JSON: %w", err)
	}
	// The real input is uncached + cache writes + cache reads.
	res := Result{InputTokens: o.Usage.In + o.Usage.CacheWrite + o.Usage.CacheRead, OutputTokens: o.Usage.Out,
		CacheReadTokens: o.Usage.CacheRead, CacheWriteTokens: o.Usage.CacheWrite}
	if o.Cost != nil {
		res.CostUSD, res.CostKnown = *o.Cost, true
	}
	if o.IsError {
		// A failed run (an API error, or one stopped by --max-budget-usd)
		// may still have spent money: keep its usage and cost so it is
		// charged (ruling P8-R15), but no verdict.
		return res, fmt.Errorf("claude error: %s", o.Result)
	}
	res.Raw = o.Result
	return finishResult(res, o.Result, pass)
}

// stripBudgetArgs drops every --max-budget-usd from args, in both the
// "--max-budget-usd X" and "--max-budget-usd=X" forms.
func stripBudgetArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--max-budget-usd":
			i++ // its value, whatever it looks like
		case strings.HasPrefix(args[i], "--max-budget-usd="):
		default:
			out = append(out, args[i])
		}
	}
	return out
}
