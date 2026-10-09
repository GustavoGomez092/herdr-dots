package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"herdr-dots/internal/platform"
)

// privateIndex copies repo's index into a private temp file and makes the
// copy match the working tree for paths, so `git apply --3way` (which
// implies --index) sees the user's current files, staged or not, as "ours"
// and never writes the user's real index. The caller removes the directory.
func privateIndex(ctx context.Context, g Git, dir, repo string, paths []string) (env []string, err error) {
	idx := filepath.Join(dir, "index")
	env = []string{"GIT_INDEX_FILE=" + idx}
	seeded := false
	if src, err := g.Run(ctx, repo, nil, "rev-parse", "--path-format=absolute", "--git-path", "index"); err == nil {
		seeded = copyFile(filepath.FromSlash(src), idx) == nil
	}
	if !seeded {
		if _, err := g.Run(ctx, repo, env, "read-tree", "HEAD"); err != nil {
			return nil, err
		}
	}
	if len(paths) > 0 {
		// --add/--remove: a path that exists nowhere is skipped silently.
		args := append([]string{"update-index", "--add", "--remove", "--"}, paths...)
		if _, err := g.Run(ctx, repo, env, args...); err != nil {
			return nil, err
		}
	}
	return env, nil
}

// Apply applies diff to repo's working tree only: the index it uses is a
// private copy, so nothing is staged and nothing is committed, and HEAD,
// refs and the stash are never touched. diff is the hash-verified content
// from patchstore.Diff; it is written to a private file here and never
// re-read from the patch store. Where the working tree moved since the
// snapshot it merges three ways; files it couldn't merge cleanly keep
// conflict markers and are returned in conflicts with a nil error. A hard
// failure (a file the patch edits is gone, say) applies nothing.
func Apply(ctx context.Context, g Git, repo string, diff []byte, paths []string) (conflicts []string, err error) {
	dir, err := os.MkdirTemp("", "herdr-dots-apply-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := platform.SecureDir(dir); err != nil {
		return nil, err
	}
	patchPath := filepath.Join(dir, "change.patch")
	if err := os.WriteFile(patchPath, diff, 0o600); err != nil {
		return nil, err
	}
	env, err := privateIndex(ctx, g, dir, repo, paths)
	if err != nil {
		return nil, err
	}
	_, aerr := g.Run(ctx, repo, env, "apply", "--3way", "--whitespace=nowarn", patchPath)
	if aerr == nil {
		return nil, nil
	}
	out, uerr := g.Run(ctx, repo, env, "diff", "--name-only", "--diff-filter=U")
	if uerr == nil && out != "" {
		return strings.Split(out, "\n"), nil
	}
	return nil, errors.Join(aerr, uerr)
}
