package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/platform"
)

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
	// Cleanup removes files BuildCmd wrote for this command (a long system
	// prompt); nil when there are none. Callers run it through Close.
	Cleanup func()
}

// Close runs Cleanup when it is set; safe on any Cmd.
func (c Cmd) Close() {
	if c.Cleanup != nil {
		c.Cleanup()
	}
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (stdout, stderr []byte, err error)
}

// ExecRunner runs commands as a process tree (a process group on Unix, a Job
// Object on Windows) so a timeout kills the CLI and everything it spawned.
// On Windows a .cmd/.bat shim runs through cmd.exe with every argument
// quoted; an argument cmd.exe can't carry, or a line over its limit, fails
// before anything starts (platform.ErrUnsafeBatchArg, platform.ErrCmdTooLong).
type ExecRunner struct{}

// ErrNoRunner fails a CLI run whose backend was built without a Runner (B6).
var ErrNoRunner = errors.New("backend has no command runner")

func (ExecRunner) Run(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err // already cancelled: start nothing (B5)
	}
	cmd, err := platform.Command(c.Name, c.Args)
	if err != nil {
		return nil, nil, err
	}
	cmd.Dir = c.Dir
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	// CleanEnv here too (S6), not only in the backends that build Env:
	// Herdr's variables never reach a dot's CLI. Windows: the child starts
	// in the project, and an npm shim's bare "node" must not find a
	// repo-local node.exe/.cmd there (platform.ChildEnv).
	cmd.Env = platform.ChildEnv(CleanEnv(env))
	cmd.Stdin = strings.NewReader(c.Stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	tree, err := platform.StartTree(cmd)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.Bytes(), errb.Bytes(), err
	case <-ctx.Done():
		_ = tree.Kill()
		<-done
		return out.Bytes(), errb.Bytes(), ctx.Err()
	}
}

// Preflight reports whether err stopped a command before it started (a
// Windows .cmd shim argument or length problem); its message is the fix.
func Preflight(err error) bool {
	return errors.Is(err, platform.ErrCmdTooLong) || errors.Is(err, platform.ErrUnsafeBatchArg)
}

// CleanEnv drops variables that would make a dot's CLI talk to Herdr or think
// it runs nested inside (or joins) another Claude Code session: HERDR_*,
// CLAUDECODE and every CLAUDE_CODE_* variable except the developer's own
// settings (platform.ClaudeCodeSetting: provider, auth, Git Bash path), the
// same rule as the engine's environment. Other CLAUDE_* variables that
// carry config (CLAUDE_CONFIG_DIR, etc.) stay. Names compare in
// platform.EnvKey form, so case-insensitively on Windows.
func CleanEnv(env []string) []string {
	// Non-nil on purpose: a nil exec.Cmd.Env inherits the full parent environment.
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		k := platform.EnvKey(name)
		if strings.HasPrefix(k, "HERDR_") || k == "CLAUDECODE" ||
			(strings.HasPrefix(k, "CLAUDE_CODE_") && !platform.ClaudeCodeSetting(k)) {
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
