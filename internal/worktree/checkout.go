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

// Checkout is a draft's detached worktree.
type Checkout struct {
	Dir  string
	Repo string
	// Defanged maps the slash-separated path of every symlink that pointed
	// outside the repository to its target. Add replaces them with a note
	// file for the draft, so no tool can write through them; Capture puts
	// them back before diffing.
	Defanged map[string]string
}

// DefangNote is the content of a file that stands in for an escaping symlink.
const DefangNote = "herdr-dots: this symlink points outside the repository, so it is replaced while the dot drafts.\n"

// Add checks the snapshot out into dir (which must not exist) as a detached
// worktree, with hooks off and LFS files left as pointers, then defangs
// symlinks that point outside the repository.
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
	if _, err := g.Run(ctx, s.Repo, nil, "worktree", "add", "--detach", dir, s.Commit); err != nil {
		return Checkout{}, err
	}
	co := Checkout{Dir: dir, Repo: s.Repo, Defanged: map[string]string{}}
	out, err := g.Output(ctx, dir, nil, "ls-files", "-s", "-z")
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

// restoreDefanged puts the defanged symlinks back. It returns the paths whose
// stand-in the dot changed: their edits are dropped.
func restoreDefanged(co Checkout) ([]string, error) {
	var touched []string
	for rel, target := range co.Defanged {
		p := filepath.Join(co.Dir, filepath.FromSlash(rel))
		if data, err := os.ReadFile(p); err != nil || string(data) != DefangNote {
			touched = append(touched, rel)
		}
		if err := os.RemoveAll(p); err != nil {
			return touched, err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return touched, err
		}
		if err := os.Symlink(target, p); err != nil {
			return touched, err
		}
	}
	return touched, nil
}

// Remove deletes a draft's worktree and its metadata in repo. If git can't
// remove it, the directory is deleted and the metadata pruned.
func Remove(ctx context.Context, g Git, repo, dir string) error {
	_, err := g.Run(ctx, repo, nil, "worktree", "remove", "--force", "--force", dir)
	if err == nil {
		return nil
	}
	rerr := os.RemoveAll(dir)
	_, perr := g.Run(ctx, repo, nil, "worktree", "prune")
	if _, serr := os.Lstat(dir); errors.Is(serr, fs.ErrNotExist) && perr == nil {
		return nil
	}
	return errors.Join(err, rerr, perr)
}

// Prune deletes draft worktree directories in worktreesDir that are older
// than olderThan and not live, then runs `git worktree prune` in repo ("" =
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
		if _, err := g.Run(ctx, repo, nil, "worktree", "prune"); err != nil {
			errs = append(errs, err)
		}
	}
	return n, errors.Join(errs...)
}
