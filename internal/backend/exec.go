package backend

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"herdr-dots/internal/dots"
)

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (stdout, stderr []byte, err error)
}

// ExecRunner runs commands in their own process group so a timeout kills the
// CLI and everything it spawned.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	cmd.Stdin = strings.NewReader(c.Stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.Bytes(), errb.Bytes(), err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return out.Bytes(), errb.Bytes(), ctx.Err()
	}
}

// CleanEnv drops variables that would make a dot's CLI talk to Herdr or think
// it runs nested inside another Claude Code session. Drops HERDR_*, CLAUDECODE,
// CLAUDE_CODE_ENTRYPOINT, and CLAUDE_CODE_CHILD_SESSION, but preserves other
// CLAUDE_* variables that carry auth and config (CLAUDE_CONFIG_DIR, etc.).
func CleanEnv(env []string) []string {
	// Non-nil on purpose: a nil exec.Cmd.Env inherits the full parent environment.
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "HERDR_") || k == "CLAUDECODE" || k == "CLAUDE_CODE_ENTRYPOINT" || k == "CLAUDE_CODE_CHILD_SESSION" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// checkArgs rejects a run whose dot args would override the read-only flags.
// BuildCmd drops them anyway; failing the run makes the problem visible.
func checkArgs(kind string, args []string) error {
	if bad := dots.DeniedArgs(kind, args); len(bad) > 0 {
		return fmt.Errorf("backend args %v are not allowed: they would override the read-only safety flags", bad)
	}
	return nil
}

// Hint turns CLI failure output into a fix the user can act on.
func Hint(kind, output string) string {
	low := strings.ToLower(output)
	if strings.Contains(low, "not logged in") || strings.Contains(low, "login") || strings.Contains(low, "unauthorized") || strings.Contains(low, "401") || strings.Contains(low, "authenticat") {
		if kind == "codex-cli" {
			return "codex is not logged in: run `codex login`"
		}
		return "claude is not logged in: run `claude` and type /login"
	}
	out := strings.TrimSpace(output)
	if r := []rune(out); len(r) > 300 {
		out = string(r[:300]) + "…"
	}
	if out == "" {
		out = "the CLI exited with an error and no output"
	}
	return out
}
