package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// outerWithMemory is nestedRepos with the outer project recorded and holding
// one project entry; it returns the outer project.
func outerWithMemory(t *testing.T) (*memCLI, string, memory.Project) {
	t.Helper()
	c, outer, _ := nestedRepos(t)
	op := resolved(t, outer)
	if err := c.store.SeeProject(op); err != nil {
		t.Fatal(err)
	}
	c.cwd = outer
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Outer app convention.")
	return c, outer, op
}

func outerIntact(t *testing.T, c *memCLI, op memory.Project, what string) {
	t.Helper()
	if got := projectTexts(t, c, op.ID); len(got) != 1 || got[0] != "Outer app convention." {
		t.Fatalf("%s: the outer project's memory is %q", what, got)
	}
}

// Round 2 (M2 fallback): git gives these nested repositories only a path
// identity, so the CLI must not fall back to the outer project whose root
// merely contains them. A purge of their path never touches the outer
// project, and an add never lands in it.
func TestNestedRepoWithAPathIdentityNeverFallsBackToTheOuterProject(t *testing.T) {
	cases := map[string]func(t *testing.T, outer string) string{
		"git init, no commit": func(t *testing.T, outer string) string {
			dir := filepath.Join(outer, "vendor", "fresh")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			gitIn(t, dir, "init", "-q", "-b", "main")
			return dir
		},
		"shallow vendored clone": func(t *testing.T, outer string) string {
			src := filepath.Join(t.TempDir(), "src")
			if err := os.MkdirAll(src, 0o700); err != nil {
				t.Fatal(err)
			}
			gitIn(t, src, "init", "-q", "-b", "main")
			for _, c := range []string{"one", "two"} {
				if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte(c), 0o600); err != nil {
					t.Fatal(err)
				}
				gitIn(t, src, "add", "f.txt")
				gitIn(t, src, "commit", "-q", "-m", c)
			}
			dir := filepath.Join(outer, "vendor", "shallow")
			gitIn(t, outer, "clone", "-q", "--depth", "1", "file://"+filepath.ToSlash(src), dir)
			return dir
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			c, outer, op := outerWithMemory(t)
			dir := mk(t, outer)
			if pr := resolved0(t, dir); !strings.HasPrefix(pr.ID, "p-") {
				t.Fatalf("the setup should give a path identity, got %+v", pr)
			}
			rel, _ := filepath.Rel(outer, dir)
			_ = c.run([]string{"purge", "--project", "./" + filepath.ToSlash(rel), "--yes"})
			outerIntact(t, c, op, "after purging "+rel)
			c.cwd = dir
			mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Nested convention.")
			outerIntact(t, c, op, "after an add in "+rel)
		})
	}
}

// Git timing out (or failing) also yields a path identity for the cwd; the
// nearest .git on disk still marks the nested repository's boundary.
func TestGitFailureInANestedRepoNeverFallsBackToTheOuterProject(t *testing.T) {
	c, outer, op := outerWithMemory(t)
	inner := filepath.Join(outer, "vendor", "lib")
	c.resolve = func(dir string) (memory.Project, error) {
		return memory.Project{ID: "p-00000000000000ee", Name: filepath.Base(dir), Root: dir}, os.ErrDeadlineExceeded
	}
	_ = c.run([]string{"purge", "--project", "./vendor/lib", "--yes"})
	outerIntact(t, c, op, "after purging vendor/lib with git timing out")
	c.cwd = filepath.Join(inner)
	mustRun(t, c, "add", "--scope", "project", "--kind", "convention", "Nested convention.")
	outerIntact(t, c, op, "after an add in vendor/lib with git timing out")
}

// A mistyped path is refused before anything is resolved.
func TestMemoryPurgeRefusesAPathThatDoesNotExist(t *testing.T) {
	c, _, op := outerWithMemory(t)
	err := c.run([]string{"purge", "--project", "./vendr/lib", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Fatalf("purge of a mistyped path: %v", err)
	}
	outerIntact(t, c, op, "after purging a mistyped path")
}

func resolved0(t *testing.T, dir string) memory.Project {
	t.Helper()
	pr, _ := memory.ResolveProject(t.Context(), dir, memory.ExecGit)
	return pr
}
