package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/platform"
)

// ErrDotGitChanged means the draft's .git file no longer says what Add wrote.
// The model can rewrite it, so git is not run on the strength of it.
var ErrDotGitChanged = errors.New("the draft's .git file was changed")

// cleanupTimeout bounds the cleanup of a failed Add, which can't reuse the
// caller's (possibly spent) context.
const cleanupTimeout = 30 * time.Second

// Checkout is a draft's detached worktree.
type Checkout struct {
	Dir  string
	Repo string
	// Defanged maps the slash-separated path of every symlink that pointed
	// outside the repository to its target. Add replaces them with a note
	// file for the draft, so no tool can write through them; Capture puts
	// them back before diffing.
	Defanged map[string]string

	git    Git    // the runner with every filter driver switched off
	gitDir string // the worktree's real git directory, as git reported it after add
	dotGit []byte // the content Add found in <Dir>/.git
}

// DefangNote is the content of a file that stands in for an escaping symlink.
const DefangNote = "herdr-dots: this symlink points outside the repository, so it is replaced while the dot drafts.\n"

// Add checks the snapshot out into dir (which must not exist) as a detached
// worktree, with hooks and filter drivers off and LFS files left as pointers,
// then defangs symlinks that point outside the repository. If git fails or
// times out, whatever it registered (a locked entry included) is removed.
func Add(ctx context.Context, g Git, s Snapshot, dir string) (Checkout, error) {
	if !ValidID(s.ID) {
		return Checkout{}, fmt.Errorf("invalid draft id %q", s.ID)
	}
	if _, err := os.Lstat(dir); err == nil {
		return Checkout{}, fmt.Errorf("worktree directory %s already exists", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return Checkout{}, err
	}
	// The checkout holds a copy of the user's code: its parent stays private
	// (0700 on Unix, an owner-only ACL on Windows that the checkout inherits).
	if err := platform.SecureDir(filepath.Dir(dir)); err != nil {
		return Checkout{}, err
	}
	// Filters are the user's own code, but the draft's files and attributes are
	// model-written: no driver runs in the checkout or in anything after it.
	gf, err := g.WithoutFilters(ctx, s.Repo)
	if err != nil {
		return Checkout{}, err
	}
	fail := func(err error) (Checkout, error) {
		// The caller's context may be the thing that expired: use a fresh one.
		cctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_ = Remove(cctx, g, s.Repo, dir)
		return Checkout{}, err
	}
	if _, err := gf.Run(ctx, s.Repo, nil, "worktree", "add", "--detach", dir, s.Commit); err != nil {
		return fail(err)
	}
	dotGit, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return fail(err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(dotGit)), "gitdir:")
	if !ok {
		return fail(fmt.Errorf("unexpected .git file in %s", dir))
	}
	gitDir = filepath.FromSlash(strings.TrimSpace(gitDir))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	co := Checkout{Dir: dir, Repo: s.Repo, Defanged: map[string]string{}, git: gf, gitDir: gitDir, dotGit: dotGit}
	out, err := co.GitOutput(ctx, "ls-files", "-s", "-z")
	if err != nil {
		return co, err
	}
	for _, rec := range bytes.Split(out, []byte{0}) {
		meta, rel, ok := strings.Cut(string(rec), "\t")
		if !ok || !strings.HasPrefix(meta, "120000 ") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		target, err := os.Readlink(p)
		if err != nil || !Escapes(rel, target) {
			continue // core.symlinks=false checks links out as plain files
		}
		if err := os.Remove(p); err != nil {
			return co, err
		}
		if err := os.WriteFile(p, []byte(DefangNote), 0o600); err != nil {
			return co, err
		}
		co.Defanged[rel] = target
	}
	return co, nil
}

// verify checks that <Dir>/.git is still the regular file Add saw.
func (co Checkout) verify() error {
	if co.gitDir == "" {
		return fmt.Errorf("%w: the checkout was not made by Add", ErrDotGitChanged)
	}
	p := filepath.Join(co.Dir, ".git")
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not the file Add wrote", ErrDotGitChanged, p)
	}
	if now, err := os.ReadFile(p); err != nil || !bytes.Equal(now, co.dotGit) {
		return fmt.Errorf("%w: %s no longer says what Add wrote", ErrDotGitChanged, p)
	}
	return nil
}

// GitOutput runs git inside the draft and returns its raw stdout. The
// repository is named explicitly (GIT_DIR, GIT_WORK_TREE) instead of being
// discovered from <Dir>/.git, which the dot can rewrite, and every filter
// driver is off. It refuses to run if <Dir>/.git was changed.
func (co Checkout) GitOutput(ctx context.Context, args ...string) ([]byte, error) {
	if err := co.verify(); err != nil {
		return nil, err
	}
	return co.git.Output(ctx, co.Dir, []string{"GIT_DIR=" + co.gitDir, "GIT_WORK_TREE=" + co.Dir}, args...)
}

// Git is GitOutput with the stdout trimmed.
func (co Checkout) Git(ctx context.Context, args ...string) (string, error) {
	out, err := co.GitOutput(ctx, args...)
	return strings.TrimSpace(string(out)), err
}

// restoreDefanged puts the defanged symlinks back. It returns the paths whose
// stand-in the dot changed: their edits are dropped. Every parent directory is
// checked with Lstat first, so a parent the dot swapped for a symlink is
// refused instead of followed.
func restoreDefanged(co Checkout) ([]string, error) {
	var touched []string
	for rel, target := range co.Defanged {
		p := filepath.Join(co.Dir, filepath.FromSlash(rel))
		if err := ensurePlainParents(co.Dir, rel); err != nil {
			return touched, err
		}
		if data, err := os.ReadFile(p); err != nil || string(data) != DefangNote {
			touched = append(touched, rel)
		}
		if err := os.RemoveAll(p); err != nil {
			return touched, err
		}
		if err := os.Symlink(target, p); err != nil {
			return touched, err
		}
	}
	return touched, nil
}

// ensurePlainParents makes the directories above rel under root, refusing any
// existing component that is not a real directory.
func ensurePlainParents(root, rel string) error {
	cur := root
	parts := strings.Split(rel, "/")
	for _, el := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, el)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(cur, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("%w: %s is not a directory", ErrEscape, cur)
		}
	}
	return nil
}

// Remove deletes a draft's worktree and its metadata in repo. If git can't
// remove it, the directory is deleted, a lock left on its registration (a
// killed `worktree add` leaves one) is lifted and the metadata pruned.
func Remove(ctx context.Context, g Git, repo, dir string) error {
	_, err := g.Run(ctx, repo, nil, "worktree", "remove", "--force", "--force", dir)
	if err == nil {
		return nil
	}
	rerr := os.RemoveAll(dir)
	uerr := unlockMissing(ctx, g, repo, func(p string) bool { return samePath(p, dir) })
	_, perr := g.Run(ctx, repo, nil, "worktree", "prune")
	if _, serr := os.Lstat(dir); errors.Is(serr, fs.ErrNotExist) && perr == nil && uerr == nil {
		return nil
	}
	return errors.Join(err, rerr, uerr, perr)
}

// Prune deletes draft worktree directories in worktreesDir that are older
// than olderThan and not live, lifts the lock on registrations of draft
// directories that are gone, then runs `git worktree prune` in repo ("" =
// skip), which drops the metadata of worktrees whose directory is gone. It
// returns how many directories it deleted.
func Prune(ctx context.Context, g Git, repo, worktreesDir string, olderThan time.Duration, live map[string]bool) (int, error) {
	ents, err := os.ReadDir(worktreesDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	n := 0
	var errs []error
	for _, e := range ents {
		if !e.IsDir() || !ValidID(e.Name()) || live[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < olderThan {
			continue
		}
		if err := os.RemoveAll(filepath.Join(worktreesDir, e.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	if repo != "" {
		// `git worktree prune` skips locked entries, and a `worktree add` killed
		// mid-way leaves one locked ("initializing"). Unlock draft registrations
		// whose directory is gone, so prune can drop them.
		err := unlockMissing(ctx, g, repo, func(p string) bool {
			return ValidID(filepath.Base(p)) && !live[filepath.Base(p)] && samePath(filepath.Dir(p), worktreesDir)
		})
		if err != nil {
			errs = append(errs, err)
		}
		if _, err := g.Run(ctx, repo, nil, "worktree", "prune"); err != nil {
			errs = append(errs, err)
		}
	}
	return n, errors.Join(errs...)
}

// unlockMissing unlocks every locked worktree registration of repo whose
// directory no longer exists and whose path match accepts.
func unlockMissing(ctx context.Context, g Git, repo string, match func(path string) bool) error {
	list, err := g.Run(ctx, repo, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	var errs []error
	for _, block := range strings.Split(list, "\n\n") {
		var path string
		locked := false
		for _, line := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				path = filepath.FromSlash(p)
			} else if line == "locked" || strings.HasPrefix(line, "locked ") {
				locked = true
			}
		}
		if path == "" || !locked || !match(path) {
			continue
		}
		if _, err := os.Lstat(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if _, err := g.Run(ctx, repo, nil, "worktree", "unlock", path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// samePath compares two paths that may differ in symlinked ancestors (macOS
// /var vs /private/var): the last element must match and the parents must
// resolve to the same directory.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if filepath.Base(a) != filepath.Base(b) {
		return false
	}
	ra, erra := filepath.EvalSymlinks(filepath.Dir(a))
	rb, errb := filepath.EvalSymlinks(filepath.Dir(b))
	return erra == nil && errb == nil && ra == rb
}
