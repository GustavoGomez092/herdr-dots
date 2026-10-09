package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Project identifies the repository a pane works in.
type Project struct {
	ID   string // g-<16 hex of the root commit> or p-<16 hex of sha256(realpath)>
	Name string
	Root string
}

// GitRunner runs git in dir and returns trimmed stdout.
type GitRunner func(ctx context.Context, dir string, args ...string) (string, error)

// gitTimeout bounds one ResolveProject call; tests shorten it.
var gitTimeout = 5 * time.Second

// ExecGit runs git with hooks disabled, no terminal prompts and no optional locks.
func ExecGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// ResolveProject identifies the project cwd belongs to: the lowest root commit
// of its git repository (so clones, moves and worktrees share one id), or a
// hash of the real path when that isn't available (not a repo, no commits
// yet, shallow clone). The returned Project is always usable; a non-nil error
// explains a path fallback. Only an empty cwd yields a zero Project.
func ResolveProject(ctx context.Context, cwd string, git GitRunner) (Project, error) {
	if strings.TrimSpace(cwd) == "" {
		return Project{}, errors.New("no working directory")
	}
	if git == nil {
		git = ExecGit
	}
	real := realPath(cwd)
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	top, err := git(ctx, real, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return pathProject(real), fmt.Errorf("not a git repository (%v): using path identity", err)
	}
	top = realPath(top)
	if sh, err := git(ctx, top, "rev-parse", "--is-shallow-repository"); err == nil && sh == "true" {
		return pathProject(top), errors.New("shallow clone (root commit unstable): using path identity")
	}
	out, err := git(ctx, top, "rev-list", "--max-parents=0", "HEAD")
	roots := strings.Fields(out)
	if err != nil || len(roots) == 0 {
		return pathProject(top), fmt.Errorf("no commits yet (%v): using path identity", err)
	}
	sort.Strings(roots)
	name := filepath.Base(top)
	if common, err := git(ctx, top, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && filepath.Base(common) == ".git" {
		name = filepath.Base(filepath.Dir(common)) // a worktree shows its main repo's name
	}
	return Project{ID: "g-" + roots[0][:min(16, len(roots[0]))], Name: name, Root: top}, nil
}

func realPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func pathProject(real string) Project {
	sum := sha256.Sum256([]byte(real))
	return Project{ID: "p-" + hex.EncodeToString(sum[:])[:16], Name: filepath.Base(real), Root: real}
}
