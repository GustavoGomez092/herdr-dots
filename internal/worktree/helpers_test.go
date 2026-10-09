package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv isolates git from the developer's config: a private HOME and XDG
// dir, no system config.
func testEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" || k == "XDG_CONFIG_HOME" || k == "USERPROFILE" || strings.HasPrefix(k, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GIT_CONFIG_NOSYSTEM=1")
}

// isoGit is the runner under test, with the isolated environment.
func isoGit(t *testing.T) Git {
	t.Helper()
	env := testEnv(t)
	return Git{Environ: func() []string { return env }}
}

// sh runs setup git commands (not the runner under test), hooks off.
func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false",
		"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = testEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newRepo makes a repository with one commit: a.txt (ten lines), b.txt,
// c.txt and a .gitignore for *.log and build/.
func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	sh(t, repo, "init", "-q")
	writeFile(t, filepath.Join(repo, "a.txt"), "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n")
	writeFile(t, filepath.Join(repo, "c.txt"), "c\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.log\nbuild/\n")
	sh(t, repo, "add", ".")
	sh(t, repo, "commit", "-qm", "init")
	return repo
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// installMarkerHooks makes every hook that git could run write a marker file.
func installMarkerHooks(t *testing.T, repo string) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	for _, h := range []string{"post-checkout", "post-index-change", "reference-transaction", "pre-commit", "post-commit", "pre-applypatch", "post-applypatch"} {
		p := filepath.Join(repo, ".git", "hooks", h)
		writeFile(t, p, "#!/bin/sh\necho "+h+" >> '"+filepath.ToSlash(marker)+"'\n")
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return marker
}

func snapshotT(t *testing.T, g Git, repo string) Snapshot {
	t.Helper()
	s, err := TakeSnapshot(context.Background(), g, repo, NewID(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func checkoutT(t *testing.T, g Git, s Snapshot) Checkout {
	t.Helper()
	co, err := Add(context.Background(), g, s, filepath.Join(t.TempDir(), "worktrees", s.ID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Remove(context.Background(), g, s.Repo, co.Dir) })
	return co
}
