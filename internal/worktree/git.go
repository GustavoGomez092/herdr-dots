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
	"slices"
	"strconv"
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

	// NoFilters names the filter drivers to switch off on every command
	// (filter.<n>.clean/smudge/process emptied, required=false). Set it
	// with WithoutFilters.
	NoFilters []string

	// Draft marks the runner for commands inside a draft worktree (set by
	// WithoutFilters): submodules are ignored by diff and never recursed into.
	Draft bool

	// LFSSmudge leaves GIT_LFS_SKIP_SMUDGE unset, so the user's own LFS
	// filter runs. Only Apply uses it: it writes into the user's repository.
	LFSSmudge bool

	// Config adds config overrides (key, value) through the environment.
	Config [][2]string
}

var (
	// ErrDenied means the patch touches a path the dot may not read or write
	// (secrets such as .env or id_rsa): Caps.Deny said so.
	ErrDenied = errors.New("the patch touches a denied path")
	// ErrNoDenyCheck means Validate got no Caps.Deny: it fails closed.
	ErrNoDenyCheck = errors.New("worktree: no deny check configured")
)

// WithoutFilters returns g with every filter driver configured for repo
// switched off. Use it for every command that runs in a draft worktree: a
// filter named in the user's config is the user's own code, but the draft's
// files (and its .gitattributes) are model-written. The snapshot is taken
// with the plain Git so the user's clean filters still define blob content.
func (g Git) WithoutFilters(ctx context.Context, repo string) (Git, error) {
	out, err := g.Output(ctx, repo, nil, "config", "-z", "--name-only", "--get-regexp", `^filter\.`)
	if err != nil {
		var ce *CmdError
		if !errors.As(err, &ce) || strings.TrimSpace(ce.Stderr) != "" {
			return g, err
		}
		out = nil // exit 1 with no output: no filters configured
	}
	seen := map[string]bool{}
	var names []string
	// -z: a filter name may hold whitespace or "=", so keys are NUL-separated.
	for _, key := range strings.Split(string(out), "\x00") {
		rest := strings.TrimPrefix(key, "filter.")
		i := strings.LastIndex(rest, ".")
		if i <= 0 || rest == key {
			continue
		}
		if name := rest[:i]; !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	g.NoFilters = append([]string(nil), names...)
	g.Draft = true
	return g, nil
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
	return g.OutputIn(ctx, dir, env, nil, args...)
}

// OutputIn is Output with stdin fed to git.
func (g Git) OutputIn(ctx context.Context, dir string, env []string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout())
	defer cancel()
	full := []string{"-C", dir,
		"-c", "core.hooksPath=" + platform.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
		"-c", "core.quotePath=false",
	}
	full = append(full, args...)
	// platform.CommandContext, not exec: on Windows it resolves git.exe on
	// PATH (never a repo-local git.exe in the cwd) and runs a .cmd shim safely.
	cmd, err := platform.CommandContext(ctx, g.bin(), full)
	if err != nil {
		return nil, &CmdError{Args: args, Err: err}
	}
	base := gitEnv(g.environ())
	if g.LFSSmudge {
		base = slices.DeleteFunc(base, func(kv string) bool { return strings.HasPrefix(kv, "GIT_LFS_SKIP_SMUDGE=") })
	}
	cmd.Env = platform.ChildEnv(append(append(base, g.configEnv()...), env...))
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.WaitDelay = 500 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	platform.HideWindow(cmd)
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return stdout.Bytes(), fmt.Errorf("%w (git %s, %s)", ErrTimeout, args[0], g.timeout())
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return stdout.Bytes(), fmt.Errorf("git %s: %w", args[0], ctx.Err())
	}
	if err != nil {
		return stdout.Bytes(), &CmdError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// configEnv carries the draft overrides as GIT_CONFIG_COUNT/KEY_n/VALUE_n, not
// `-c` arguments: a filter name holding "=" or whitespace would be split or
// misread on a command line, while a key from the environment is taken whole.
func (g Git) configEnv() []string {
	kv := append([][2]string(nil), g.Config...)
	for _, n := range g.NoFilters {
		for _, k := range []string{"clean", "smudge", "process"} {
			kv = append(kv, [2]string{"filter." + n + "." + k, ""})
		}
		kv = append(kv, [2]string{"filter." + n + ".required", "false"})
	}
	if g.Draft {
		kv = append(kv, [2]string{"diff.ignoreSubmodules", "all"}, [2]string{"submodule.recurse", "false"})
	}
	if len(kv) == 0 {
		return nil
	}
	env := []string{"GIT_CONFIG_COUNT=" + strconv.Itoa(len(kv))}
	for i, p := range kv {
		n := strconv.Itoa(i)
		env = append(env, "GIT_CONFIG_KEY_"+n+"="+p[0], "GIT_CONFIG_VALUE_"+n+"="+p[1])
	}
	return env
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
