package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

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
	if _, err := exec.LookPath(c.bin()); err != nil {
		return fmt.Errorf("claude CLI not found on PATH: install Claude Code (https://claude.com/claude-code)")
	}
	return nil
}

// BuildCmd: print mode, JSON output, no saved session, no MCP servers, hooks
// disabled (so Herdr's integration hooks never fire for a dot), read-only tools.
func (c ClaudeCLI) BuildCmd(r Request) Cmd {
	args := []string{
		"-p", "--output-format", "json",
		"--no-session-persistence",
		"--strict-mcp-config",
		"--settings", `{"disableAllHooks":true}`,
		"--append-system-prompt", SystemPrompt(r),
	}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	if r.RepoRead {
		args = append(args, "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob")
	} else {
		args = append(args, "--tools", "")
	}
	args = append(args, r.Args...)
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: UserPrompt(r)}
}

func (c ClaudeCLI) Run(ctx context.Context, r Request) (Result, error) {
	out, stderr, err := c.Runner.Run(ctx, c.BuildCmd(r))
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("claude timed out: %w", ctx.Err())
	}
	res, perr := ParseClaudeOutput(out)
	if err != nil && perr != nil {
		return Result{}, fmt.Errorf("claude failed: %s", Hint(c.Kind(), string(stderr)+"\n"+string(out)))
	}
	return res, perr
}

func ParseClaudeOutput(out []byte) (Result, error) {
	var o struct {
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		Usage   struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &o); err != nil {
		return Result{}, fmt.Errorf("claude output is not JSON: %w", err)
	}
	if o.IsError {
		return Result{}, fmt.Errorf("claude error: %s", o.Result)
	}
	res := Result{InputTokens: o.Usage.In, OutputTokens: o.Usage.Out, CostUSD: o.Cost, Raw: o.Result}
	v, err := ParseVerdict(o.Result)
	res.Verdict = v
	return res, err
}
