package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

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
	Runner  Runner
	Bin     string          // default "claude"
	Environ func() []string // default os.Environ
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
	if WriteMode(r) {
		settings = writeSettings
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
	case r.RepoRead:
		args = append(args, "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob")
	default:
		args = append(args, "--tools", "")
	}
	// Denied args (dots.Validate rejects them too) are dropped so they can never
	// override the flags above.
	args = append(args, dots.SafeArgs(c.Kind(), r.Args)...)
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: UserPrompt(r), Cleanup: cleanup}
}

func (c ClaudeCLI) Run(ctx context.Context, r Request) (Result, error) {
	if err := checkArgs(c.Kind(), r.Args); err != nil {
		return Result{}, err
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
		return Result{}, fmt.Errorf("claude failed: %s", Hint(c.Kind(), string(stderr)+"\n"+string(out)))
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
	if o.IsError {
		return Result{}, fmt.Errorf("claude error: %s", o.Result)
	}
	// The real input is uncached + cache writes + cache reads.
	res := Result{InputTokens: o.Usage.In + o.Usage.CacheWrite + o.Usage.CacheRead, OutputTokens: o.Usage.Out,
		CacheReadTokens: o.Usage.CacheRead, CacheWriteTokens: o.Usage.CacheWrite, Raw: o.Result}
	if o.Cost != nil {
		res.CostUSD, res.CostKnown = *o.Cost, true
	}
	return finishResult(res, o.Result, pass)
}
