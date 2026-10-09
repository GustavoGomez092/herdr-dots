package worktree

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"herdr-dots/internal/platform"
)

// RefPrefix holds one private ref per unresolved draft. The ref keeps the
// snapshot's objects (the patch's preimage blobs) alive until the patch is
// applied, sent and expired, or discarded.
const RefPrefix = "refs/herdr-dots/snapshots/"

// Snapshot is a commit of the working tree as it was when a draft started:
// tracked files with their unstaged and staged changes, plus untracked files
// that aren't ignored, parented on HEAD.
type Snapshot struct {
	ID         string `json:"id"`
	Repo       string `json:"repo"`      // the repository's top-level directory
	Commit     string `json:"commit"`    // the snapshot commit
	Ref        string `json:"ref"`       // RefPrefix + ID
	BaseHead   string `json:"base_head"` // HEAD when the snapshot was taken
	Submodules bool   `json:"submodules,omitempty"`
}

// RepoRoot returns the top-level directory of the repository dir is in.
func RepoRoot(ctx context.Context, g Git, dir string) (string, error) {
	top, err := g.Run(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if errors.Is(err, ErrTimeout) {
		return "", err
	}
	if err != nil || top == "" {
		return "", fmt.Errorf("%w: %s", ErrNotGit, dir)
	}
	return filepath.Clean(filepath.FromSlash(top)), nil
}

// TakeSnapshot commits the working tree of the repository at repo through a
// private copy of its index, so the user's index, stash, branches and HEAD
// never change. maxIndexBytes caps the private index (0 = no cap). A slow
// `git add` (over the Git timeout) or an index over the cap is ErrTooLarge.
func TakeSnapshot(ctx context.Context, g Git, repo, id string, maxIndexBytes int64) (Snapshot, error) {
	if !ValidID(id) {
		return Snapshot{}, fmt.Errorf("invalid draft id %q", id)
	}
	top, err := RepoRoot(ctx, g, repo)
	if err != nil {
		return Snapshot{}, err
	}
	head, err := g.Run(ctx, top, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if errors.Is(err, ErrTimeout) {
		return Snapshot{}, err
	}
	if err != nil || head == "" {
		return Snapshot{}, ErrUnbornHead
	}
	tmp, err := os.MkdirTemp("", "herdr-dots-index-")
	if err != nil {
		return Snapshot{}, err
	}
	defer os.RemoveAll(tmp)
	// The private index names every file; keep it the user's alone (on
	// Windows a temp dir inherits the parent's ACL, not 0700).
	if err := platform.SecureDir(tmp); err != nil {
		return Snapshot{}, err
	}
	idx := filepath.Join(tmp, "index")
	// Seeding from the user's index (a read-only copy) lets git reuse its
	// stat cache, so only changed files are hashed.
	if src, err := g.Run(ctx, top, nil, "rev-parse", "--path-format=absolute", "--git-path", "index"); err == nil {
		_ = copyFile(filepath.FromSlash(src), idx)
	}
	env := []string{"GIT_INDEX_FILE=" + idx}
	if _, err := g.Run(ctx, top, env, "add", "-A", "--", "."); err != nil {
		if errors.Is(err, ErrTimeout) {
			return Snapshot{}, fmt.Errorf("%w: working tree too large to snapshot; commit or ignore build output", ErrTooLarge)
		}
		// A seed git can't use (split index, a newer format): start empty.
		_ = os.Remove(idx)
		if _, err := g.Run(ctx, top, env, "add", "-A", "--", "."); err != nil {
			if errors.Is(err, ErrTimeout) {
				return Snapshot{}, fmt.Errorf("%w: working tree too large to snapshot; commit or ignore build output", ErrTooLarge)
			}
			return Snapshot{}, err
		}
	}
	if st, err := os.Stat(idx); err == nil && maxIndexBytes > 0 && st.Size() > maxIndexBytes {
		return Snapshot{}, fmt.Errorf("%w: the snapshot index is %d MB (max %d MB); commit or ignore build output", ErrTooLarge, st.Size()>>20, maxIndexBytes>>20)
	}
	tree, err := g.Run(ctx, top, env, "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	commit, err := g.Run(ctx, top, identEnv(), "commit-tree", "--no-gpg-sign", "-p", head, "-m", "herdr-dots snapshot "+id, tree)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{ID: id, Repo: top, Commit: commit, Ref: RefPrefix + id, BaseHead: head}
	if _, err := g.Run(ctx, top, nil, "update-ref", s.Ref, commit); err != nil {
		return Snapshot{}, err
	}
	if _, err := os.Stat(filepath.Join(top, ".gitmodules")); err == nil {
		s.Submodules = true
	}
	return s, nil
}

// DropSnapshot deletes the snapshot's private ref. A missing ref is fine.
func DropSnapshot(ctx context.Context, g Git, repo, id string) error {
	if !ValidID(id) {
		return fmt.Errorf("invalid draft id %q", id)
	}
	_, err := g.Run(ctx, repo, nil, "update-ref", "-d", RefPrefix+id)
	return err
}

// SnapshotIDs lists the draft ids that still have a private ref in repo.
func SnapshotIDs(ctx context.Context, g Git, repo string) ([]string, error) {
	out, err := g.Run(ctx, repo, nil, "for-each-ref", "--format=%(refname)", RefPrefix)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), RefPrefix); ok && ValidID(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	return errors.Join(cerr, out.Close())
}
