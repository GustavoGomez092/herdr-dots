package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

// nestedRepos makes an outer repository with an independent repository
// vendored inside it at vendor/lib, both with a commit (so both get a g- id),
// and a memory CLI whose resolver is the real git one. Git's own config is
// kept out of the test.
func nestedRepos(t *testing.T) (c *memCLI, outer, inner string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	commit := func(dir, content string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q", "-b", "main"}} {
			if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "f.txt"}, {"commit", "-q", "-m", content}} {
			if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	root := t.TempDir()
	outer = filepath.Join(root, "a-app")
	commit(outer, "outer")
	inner = filepath.Join(outer, "vendor", "lib")
	commit(inner, "inner")
	c = &memCLI{
		store: memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits()),
		resolve: func(dir string) (memory.Project, error) {
			return memory.ResolveProject(context.Background(), dir, memory.ExecGit)
		},
		cwd: outer, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, in: strings.NewReader(""),
	}
	return c, outer, inner
}

func resolved(t *testing.T, dir string) memory.Project {
	t.Helper()
	pr, err := memory.ResolveProject(context.Background(), dir, memory.ExecGit)
	if err != nil || !strings.HasPrefix(pr.ID, "g-") {
		t.Fatalf("resolve %s: %+v %v", dir, pr, err)
	}
	return pr
}

func projectTexts(t *testing.T, c *memCLI, pid string) []string {
	t.Helper()
	es, err := c.store.List(pid, memory.Filter{Scope: memory.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Text)
	}
	return out
}

// Only the outer repository was ever attached in Herdr (recorded with
// SeeProject). `memory add` run inside the vendored repository files the
// entry under the vendored repository's own project, not the outer one.
func TestMemoryAddInAnUnattachedNestedRepoUsesTheNestedProject(t *testing.T) {
	c, outer, inner := nestedRepos(t)
	op, ip := resolved(t, outer), resolved(t, inner)
	if err := c.store.SeeProject(op); err != nil {
		t.Fatal(err)
	}
	c.cwd = filepath.Join(inner)
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Vendored lib convention.")
	if got := projectTexts(t, c, ip.ID); len(got) != 1 || got[0] != "Vendored lib convention." {
		t.Fatalf("nested project memory: %q", got)
	}
	if got := projectTexts(t, c, op.ID); len(got) != 0 {
		t.Fatalf("the outer project got the nested repository's memory: %q", got)
	}
}

// `memory purge --project ./vendor/lib`, run from the outer repository,
// deletes the vendored repository's memory and never the outer project's.
func TestMemoryPurgeOfANestedRepoPathLeavesTheOuterProject(t *testing.T) {
	c, outer, inner := nestedRepos(t)
	op, ip := resolved(t, outer), resolved(t, inner)
	if err := c.store.SeeProject(op); err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Outer app convention.")
	c.cwd = inner
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Vendored lib convention.")
	c.cwd = outer
	mustRun(t, c, "purge", "--project", "./vendor/lib", "--yes")
	if got := projectTexts(t, c, op.ID); len(got) != 1 || got[0] != "Outer app convention." {
		t.Fatalf("outer project memory after purging the nested path: %q", got)
	}
	if got := projectTexts(t, c, ip.ID); len(got) != 0 {
		t.Fatalf("nested project memory survived its purge: %q", got)
	}
}

// A path project recorded deeper than the repository root (say, from before
// the directory was a repository) is stale: the repository's g- id wins.
func TestStaleDeeperPathProjectDoesNotBeatTheRepository(t *testing.T) {
	c, outer, _ := nestedRepos(t)
	op := resolved(t, outer)
	if err := c.store.SeeProject(op); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(outer, "pkg")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := memory.Project{ID: "p-00000000000000cc", Name: "pkg", Root: sub}
	if r, err := filepath.EvalSymlinks(sub); err == nil {
		stale.Root = r
	}
	if err := c.store.SeeProject(stale); err != nil {
		t.Fatal(err)
	}
	c.cwd = sub
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Package convention.")
	if got := projectTexts(t, c, op.ID); len(got) != 1 || got[0] != "Package convention." {
		t.Fatalf("repository project memory: %q", got)
	}
	if got := projectTexts(t, c, stale.ID); len(got) != 0 {
		t.Fatalf("the stale path project won: %q", got)
	}
}

// Without a repository (path identity), a recorded project whose root holds
// the path is still found, as before.
func TestPathIdentityStillFindsARecordedProject(t *testing.T) {
	c, _, _ := nestedRepos(t)
	plain := t.TempDir()
	if r, err := filepath.EvalSymlinks(plain); err == nil {
		plain = r
	}
	rec := memory.Project{ID: "g-00000000000000dd", Name: "moved", Root: plain}
	if err := c.store.SeeProject(rec); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(plain, "src")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	c.cwd = sub
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Recorded project convention.")
	if got := projectTexts(t, c, rec.ID); len(got) != 1 {
		t.Fatalf("recorded project memory: %q", got)
	}
}
