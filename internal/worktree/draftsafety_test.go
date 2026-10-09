package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The Task 2 security review's items (a)-(e), as tests.

func TestCaptureNeverRunsFiltersWithOddNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell filter")
	}
	g := isoGit(t)
	repo := newRepo(t)
	s := snapshotT(t, g, repo)
	marker := filepath.Join(t.TempDir(), "filter-ran")
	cmd := "sh -c 'echo ran >> " + filepath.ToSlash(marker) + "; cat'"
	for _, name := range []string{"a=b", "has space"} {
		for _, k := range []string{"clean", "smudge"} {
			sh(t, repo, "config", "filter."+name+"."+k, cmd)
		}
	}
	co := checkoutT(t, g, s)
	writeFile(t, filepath.Join(co.Dir, ".gitattributes"), "*.txt filter=a=b\n*.md filter=has space\n")
	writeFile(t, filepath.Join(co.Dir, "x.txt"), "x\n")
	writeFile(t, filepath.Join(co.Dir, "y.md"), "y\n")
	p, err := Capture(context.Background(), g, co, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 3 {
		t.Fatalf("changes %v", p.Paths())
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a user filter ran in the draft: %s", b)
	}
}

func TestCaptureNeverRunsTextconvOrExternalDiff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell program")
	}
	g := isoGit(t)
	repo := newRepo(t)
	s := snapshotT(t, g, repo)
	marker := filepath.Join(t.TempDir(), "diff-ran")
	m := filepath.ToSlash(marker)
	sh(t, repo, "config", "diff.evil.textconv", "sh -c 'echo ran >> "+m+"; cat \"$1\"' x")
	sh(t, repo, "config", "diff.external", "sh -c 'echo ran >> "+m+"' x")
	co := checkoutT(t, g, s)
	writeFile(t, filepath.Join(co.Dir, ".gitattributes"), "*.txt diff=evil\n")
	writeFile(t, filepath.Join(co.Dir, "b.txt"), "changed\n")
	if _, err := Capture(context.Background(), g, co, s); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a diff program ran in the draft: %s", b)
	}
}

func TestDraftRunnerIgnoresSubmodulesAndDoesNotRecurse(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	gf, err := g.WithoutFilters(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"diff.ignoresubmodules": "all", "submodule.recurse": "false"} {
		got, err := gf.Run(context.Background(), repo, nil, "config", "--get", key)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, err, want)
		}
	}
	// The plain runner (the snapshot's) is left alone.
	if _, err := g.Run(context.Background(), repo, nil, "config", "--get", "diff.ignoresubmodules"); err == nil {
		t.Error("the plain runner must not set diff.ignoreSubmodules")
	}
}

func TestAddRemovesTheWorktreeWhenALaterStepFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	repo := newRepo(t)
	g := isoGit(t)
	s := snapshotT(t, g, repo)
	wrapper := filepath.Join(t.TempDir(), "failgit")
	writeFile(t, wrapper, "#!/bin/sh\ncase \"$*\" in *\" ls-files \"*) echo boom >&2; exit 1;; esac\nexec git \"$@\"\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := g
	bad.Bin = wrapper
	dir := filepath.Join(t.TempDir(), "worktrees", s.ID)
	if _, err := Add(context.Background(), bad, s, dir); err == nil {
		t.Fatal("Add should fail when ls-files fails")
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("the worktree directory was left behind: %v", err)
	}
	noWorktrees(t, repo)
}
