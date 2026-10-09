//go:build unix

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// DevNull is the null device, for git's -c core.hooksPath.
const DevNull = "/dev/null"

// HomeEnv is the variable that names the user's home directory, the one
// os.UserHomeDir and child CLIs (codex) read.
const HomeEnv = "HOME"

func herdrConfigDir(_ func(string) string, home string) string { return unixHerdrConfigDir(home) }

// EnvKey is the form environment variable names are compared in: as-is on
// Unix, where names are case-sensitive.
func EnvKey(name string) string { return name }

// LookPath finds a program on PATH (exec.LookPath).
func LookPath(name string) (string, error) { return exec.LookPath(name) }

// Command is exec.Command; the program is resolved at Start, as in v1.
func Command(name string, args []string) (*exec.Cmd, error) { return exec.Command(name, args...), nil }

// CommandContext is Command bound to ctx (exec.CommandContext).
func CommandContext(ctx context.Context, name string, args []string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, name, args...), nil
}

// HideWindow keeps a child from opening a console window. Unix: nothing to do.
func HideWindow(*exec.Cmd) {}

// StartDetached starts cmd in a new session so it outlives the hook or pane
// that launched it, and returns the started process.
func StartDetached(cmd *exec.Cmd) (*os.Process, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// Tree is a started child process together with everything it spawns.
type Tree struct{ pid int }

// StartTree starts cmd in its own process group, so Kill ends the CLI and
// every process it started.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Tree{pid: cmd.Process.Pid}, nil
}

// Kill sends SIGKILL to the whole process group.
func (t *Tree) Kill() error { return syscall.Kill(-t.pid, syscall.SIGKILL) }

// Close releases the tree. Unix: nothing to release.
func (t *Tree) Close() error { return nil }

// ErrConnRefused is the error a refused dial carries on this OS, for code
// that keeps an error's identity while dropping its text (httpx scrubbing).
var ErrConnRefused error = syscall.ECONNREFUSED

// ConnRefused reports whether a dial failed because nothing listens there.
func ConnRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
