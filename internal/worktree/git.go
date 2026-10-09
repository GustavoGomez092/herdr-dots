// Package worktree drafts patches without touching the developer's work: a
// snapshot of the working tree taken through a private index, a detached
// worktree outside the repository, the diff a dot leaves there, its
// validation, and the apply back into the working tree through a private
// index. Every git command runs with hooks and fsmonitor off and a timeout.
// Nothing here changes the user's index, stash, branches, HEAD or commits;
// the only things written to the repository are objects, the private ref
// refs/herdr-dots/snapshots/<id> and the worktree's own metadata.
package worktree

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"herdr-dots/internal/platform"
)

// DefaultTimeout bounds one git command when Git.Timeout is zero.
const DefaultTimeout = 60 * time.Second

var (
	ErrNotGit     = errors.New("not a git repository")
	ErrUnbornHead = errors.New("the repository has no commits yet: commit once first")
	ErrTimeout    = errors.New("git took too long")
	ErrTooLarge   = errors.New("too large")
	ErrEmptyPatch = errors.New("the draft produced no changes")
	ErrEscape     = errors.New("the patch reaches outside the repository")
	ErrGitDir     = errors.New("the patch touches .git")
)

// Git runs the git binary. Every command gets -c core.hooksPath=<null
// device> (no hook ever runs), core.fsmonitor=false (no fsmonitor program
// runs), gc.auto=0 and maintenance.auto=false (no background maintenance),
// and a cleaned environment.
type Git struct {
	Bin     string          // "" = "git"
	Timeout time.Duration   // per command; 0 = DefaultTimeout
	Environ func() []string // nil = os.Environ
}

func (g Git) bin() string {
	if g.Bin != "" {
		return g.Bin
	}
	return "git"
}

func (g Git) timeout() time.Duration {
	if g.Timeout > 0 {
		return g.Timeout
	}
	return DefaultTimeout
}

func (g Git) environ() []string {
	if g.Environ != nil {
		return g.Environ()
	}
	return os.Environ()
}

// CmdError is a failed git command with its (clipped) stderr.
type CmdError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CmdError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	if msg == "" {
		msg = e.Err.Error()
	}
	name := "git"
	if len(e.Args) > 0 {
		name += " " + e.Args[0]
	}
	return name + ": " + msg
}

func (e *CmdError) Unwrap() error { return e.Err }

// Run runs git in dir and returns its trimmed stdout. env adds variables
// after the cleaned environment (later values win).
func (g Git) Run(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	out, err := g.Output(ctx, dir, env, args...)
	return strings.TrimSpace(string(out)), err
}

// Output runs git in dir and returns its raw stdout.
func (g Git) Output(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout())
	defer cancel()
	full := append([]string{"-C", dir,
		"-c", "core.hooksPath=" + platform.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
		"-c", "core.quotePath=false",
	}, args...)
	// platform.CommandContext, not exec: on Windows it resolves git.exe on
	// PATH (never a repo-local git.exe in the cwd) and runs a .cmd shim safely.
	cmd, err := platform.CommandContext(ctx, g.bin(), full)
	if err != nil {
		return nil, &CmdError{Args: args, Err: err}
	}
	cmd.Env = platform.ChildEnv(append(gitEnv(g.environ()), env...))
	cmd.WaitDelay = 500 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	platform.HideWindow(cmd)
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return stdout.Bytes(), fmt.Errorf("%w (git %s, %s)", ErrTimeout, args[0], g.timeout())
	}
	if err != nil {
		return stdout.Bytes(), &CmdError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// gitEnv drops variables that would point git elsewhere (GIT_DIR,
// GIT_INDEX_FILE, GIT_CONFIG_*, ...) or make a child think it runs inside
// Herdr or Claude Code, then pins prompts, locks, lazy fetches, LFS smudging
// and the message language. Names compare in platform.EnvKey form.
func gitEnv(env []string) []string {
	out := make([]string, 0, len(env)+8)
	for _, kv := range env {
		raw, _, _ := strings.Cut(kv, "=")
		name := platform.EnvKey(raw)
		if strings.HasPrefix(name, "HERDR_") || strings.HasPrefix(name, "GIT_") {
			continue
		}
		switch name {
		case "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_CHILD_SESSION", "LC_ALL", "LANG", "LANGUAGE":
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_NO_LAZY_FETCH=1",
		"GIT_LFS_SKIP_SMUDGE=1",
		"GIT_LITERAL_PATHSPECS=1",
		"LC_ALL=C",
	)
}

// identEnv is the author and committer of snapshot commits, so commit-tree
// works without the user's user.name/user.email.
func identEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=herdr-dots", "GIT_AUTHOR_EMAIL=herdr-dots@localhost",
		"GIT_COMMITTER_NAME=herdr-dots", "GIT_COMMITTER_EMAIL=herdr-dots@localhost",
	}
}

var idRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidID reports whether id is a draft id (16 lowercase hex characters).
// Ids name directories, files and refs, so nothing else is accepted.
func ValidID(id string) bool { return idRe.MatchString(id) }

// NewID returns a random draft id.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
