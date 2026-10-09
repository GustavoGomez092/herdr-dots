package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"herdr-dots/internal/platform"
)

// tempPatch writes diff to a file in a private temp directory. The caller
// removes the directory.
func tempPatch(diff []byte) (dir, file string, err error) {
	dir, err = os.MkdirTemp("", "herdr-dots-apply-")
	if err != nil {
		return "", "", err
	}
	if err := platform.SecureDir(dir); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	file = filepath.Join(dir, "change.patch")
	if err := os.WriteFile(file, diff, 0o600); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return dir, file, nil
}

// patchPaths asks git itself which paths the diff in file would touch
// (`git apply --numstat -z`, -p1 as `git apply` will use it): the header
// paths, not the ones a Patch claims. It runs in dir, outside any repository.
func patchPaths(ctx context.Context, g Git, dir, file string) ([]string, error) {
	out, err := g.Output(ctx, dir, ceiling(dir), "apply", "--numstat", "-z", file)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		parts := strings.SplitN(string(rec), "\t", 3)
		if len(parts) != 3 || parts[2] == "" {
			return nil, fmt.Errorf("%w: unexpected git apply --numstat record %q", ErrEscape, rec)
		}
		if !slices.Contains(paths, parts[2]) {
			paths = append(paths, parts[2])
		}
	}
	if len(paths) == 0 {
		return nil, ErrEmptyPatch
	}
	return paths, nil
}

// privateIndex copies repo's index into a private temp file and makes the
// copy match the working tree for paths, so `git apply --3way` (which
// implies --index) sees the user's current files, staged or not, as "ours"
// and never writes the user's real index. Its commands run with
// core.splitIndex=false (see Apply), so no sharedindex file lands in .git.
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
		// The names go over stdin (NUL-separated), not argv.
		in := []byte(strings.Join(paths, "\x00") + "\x00")
		if _, err := g.OutputIn(ctx, repo, env, in, "update-index", "--add", "--remove", "-z", "--stdin"); err != nil {
			return nil, err
		}
	}
	return env, nil
}

// unmergedAmong lists the paths, among want, that the private index holds as
// unmerged entries. The user's own unmerged entries (a merge in progress)
// are copied into the private index and are not this patch's conflicts.
func unmergedAmong(ctx context.Context, g Git, repo string, env, want []string) ([]string, error) {
	out, err := g.Output(ctx, repo, env, "ls-files", "-u", "-z")
	if err != nil {
		return nil, err
	}
	var got []string
	for _, rec := range bytes.Split(out, []byte{0}) {
		_, path, ok := strings.Cut(string(rec), "\t")
		if ok && slices.Contains(want, path) && !slices.Contains(got, path) {
			got = append(got, path)
		}
	}
	return got, nil
}

// Apply applies diff to repo's working tree only: the index it uses is a
// private copy, so nothing is staged and nothing is committed, and HEAD,
// refs and the stash are never touched. diff is the hash-verified content
// from patchstore.Diff; it is written to a private file here and never
// re-read from the patch store. The paths Apply works on come from
// `git apply --numstat -z` on those same bytes, never from a separate list.
// From those bytes alone it refuses (ErrEscape) a rename or copy, a gitlink,
// and any block that would create or change a symlink.
//
// g must be the plain Git (Apply writes into the user's repository with the
// user's own filters; an LFS smudge filter is not skipped) and repo must be
// the repository's top-level directory: from a subdirectory `git apply`
// silently applies nothing, so Apply checks and refuses.
//
// Where the working tree moved since the snapshot it merges three ways; files
// it couldn't merge cleanly keep conflict markers and are returned in
// conflicts with a nil error. A hard failure (a file the patch edits is gone,
// say) applies nothing and is an error.
func Apply(ctx context.Context, g Git, repo string, diff []byte) (conflicts []string, err error) {
	// The user's own filters always apply: a draft runner's overrides are cleared.
	g.NoFilters, g.Draft = nil, false
	g.LFSSmudge = true
	g.Config = append(append([][2]string(nil), g.Config...), [2]string{"core.splitIndex", "false"})
	top, err := RepoRoot(ctx, g, repo)
	if err != nil {
		return nil, err
	}
	a, aerr := os.Stat(top)
	b, berr := os.Stat(repo)
	if aerr != nil || berr != nil || !os.SameFile(a, b) {
		return nil, fmt.Errorf("%s is not the repository's top-level directory (%s)", repo, top)
	}
	dir, patchPath, err := tempPatch(diff)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	// From the bytes alone: no rename or copy, no gitlink, and no symlink
	// created or changed (diffHeaders refuses those itself).
	if modes, renames, err := diffHeaders(diff); errors.Is(err, ErrEscape) {
		return nil, err
	} else if err != nil {
		return nil, fmt.Errorf("invalid patch: %w", err)
	} else if renames {
		return nil, fmt.Errorf("%w: the diff holds a rename or copy", ErrEscape)
	} else {
		for path, m := range modes {
			if m.old == "160000" || m.new == "160000" {
				return nil, fmt.Errorf("%w: %s is a submodule", ErrEscape, path)
			}
		}
	}
	paths, err := patchPaths(ctx, g, dir, patchPath)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := CheckPath(p); err != nil {
			return nil, err
		}
	}
	env, err := privateIndex(ctx, g, dir, repo, paths)
	if err != nil {
		return nil, err
	}
	_, aerr = g.Run(ctx, repo, env, "apply", "--3way", "--whitespace=nowarn", patchPath)
	if aerr == nil {
		return nil, nil
	}
	conflicts, uerr := unmergedAmong(ctx, g, repo, env, paths)
	if uerr == nil && len(conflicts) > 0 {
		return conflicts, nil
	}
	return nil, errors.Join(aerr, uerr)
}
