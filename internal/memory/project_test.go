package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
)

// isolateGit keeps the developer's git config (signing, hooks) out of tests.
func isolateGit(t *testing.T) {
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
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a repo with n commits under a fresh temp dir.
func newRepo(t *testing.T, n int) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q", "-b", "main")
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Repeat("x", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		gitT(t, dir, "add", "f.txt")
		gitT(t, dir, "commit", "-q", "-m", "c")
	}
	return dir
}

func resolve(t *testing.T, dir string) (Project, error) {
	t.Helper()
	return ResolveProject(context.Background(), dir, ExecGit)
}

func TestGitRepoIdentityIsRootCommit(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t, 2)
	pr, err := resolve(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	root := gitT(t, repo, "rev-list", "--max-parents=0", "HEAD")
	if pr.ID != "g-"+root[:16] || pr.Name != "repo" || pr.Root != realPath(repo) {
		t.Fatalf("project %+v (root %s)", pr, root)
	}
	sub := filepath.Join(repo, "pkg")
	os.MkdirAll(sub, 0o700)
	if p2, _ := resolve(t, sub); p2.ID != pr.ID {
		t.Fatalf("subdir id %s != %s", p2.ID, pr.ID)
	}
}

func TestWorktreeSharesIdentity(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t, 1)
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, repo, "worktree", "add", "-q", wt)
	a, _ := resolve(t, repo)
	b, err := resolve(t, wt)
	if err != nil || a.ID != b.ID || b.Name != "repo" {
		t.Fatalf("main %+v worktree %+v %v", a, b, err)
	}
}

func TestMovedRepoKeepsIdentity(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t, 1)
	before, _ := resolve(t, repo)
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	after, err := resolve(t, moved)
	if err != nil || after.ID != before.ID {
		t.Fatalf("before %+v after %+v %v", before, after, err)
	}
}

func TestNonGitAndEmptyRepoUsePathIdentity(t *testing.T) {
	isolateGit(t)
	plain := t.TempDir()
	pr, err := resolve(t, plain)
	if err == nil || !strings.HasPrefix(pr.ID, "p-") || len(pr.ID) != 18 || pr.Root != realPath(plain) {
		t.Fatalf("plain dir: %+v %v", pr, err)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	os.MkdirAll(empty, 0o700)
	gitT(t, empty, "init", "-q")
	pe, err := resolve(t, empty)
	if err == nil || !strings.HasPrefix(pe.ID, "p-") {
		t.Fatalf("repo without commits: %+v %v", pe, err)
	}
}

func TestShallowCloneUsesPathIdentity(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t, 3)
	dst := filepath.Join(t.TempDir(), "shallow")
	gitT(t, t.TempDir(), "clone", "-q", "--depth", "1", "file://"+repo, dst)
	pr, err := resolve(t, dst)
	if err == nil || !strings.HasPrefix(pr.ID, "p-") {
		t.Fatalf("shallow clone: %+v %v", pr, err)
	}
}

func TestExecGitNeverRunsHooks(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t, 1)
	if v, _ := ExecGit(context.Background(), repo, "config", "core.hooksPath"); v != os.DevNull {
		t.Fatalf("core.hooksPath = %q", v)
	}
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := filepath.Join(repo, ".git", "hooks", "post-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecGit(context.Background(), repo, "commit", "-q", "--allow-empty", "-m", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a repo hook ran through ExecGit")
	}
}

// Review Focus: attach must never block on a hung git.
func TestResolveProjectNeverBlocks(t *testing.T) {
	old := gitTimeout
	gitTimeout = 100 * time.Millisecond
	defer func() { gitTimeout = old }()
	hung := func(ctx context.Context, _ string, _ ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	pr, err := ResolveProject(context.Background(), t.TempDir(), hung)
	if time.Since(start) > 2*time.Second || err == nil || !strings.HasPrefix(pr.ID, "p-") {
		t.Fatalf("after %v: %+v %v", time.Since(start), pr, err)
	}
}

func TestResolveProjectNeedsCwd(t *testing.T) {
	if pr, err := ResolveProject(context.Background(), " ", ExecGit); err == nil || pr.ID != "" {
		t.Fatalf("%+v %v", pr, err)
	}
}

// An inherited GIT_DIR would override -C and identify the wrong repository.
func TestResolveProjectIgnoresInheritedGitEnv(t *testing.T) {
	isolateGit(t)
	a := newRepo(t, 1)
	b := newRepo(t, 2)
	want, err := resolve(t, a)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
	t.Setenv("GIT_WORK_TREE", b)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(b, ".git", "index"))
	got, err := resolve(t, a)
	if err != nil || got.ID != want.ID || got.Root != realPath(a) {
		t.Fatalf("got %+v %v, want %+v", got, err, want)
	}
}

func TestGitEnvStripsHerdrAndGitOverrides(t *testing.T) {
	in := []string{"PATH=/bin", "HERDR_SOCKET=x", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_CODE_CHILD_SESSION=1", "GIT_DIR=/x", "GIT_WORK_TREE=/x", "GIT_INDEX_FILE=/x",
		"GIT_COMMON_DIR=/x", "GIT_OBJECT_DIRECTORY=/x", "GIT_ALTERNATE_OBJECT_DIRECTORIES=/x",
		"GIT_CONFIG_GLOBAL=/x", "GIT_CONFIG_COUNT=1", "HOME=/h"}
	out := gitEnv(in)
	joined := "\n" + strings.Join(out, "\n") + "\n"
	for _, bad := range []string{"HERDR_", "CLAUDECODE", "CLAUDE_CODE_", "GIT_DIR=", "GIT_WORK_TREE", "GIT_INDEX_FILE",
		"GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_COUNT"} {
		if strings.Contains(joined, "\n"+bad) {
			t.Errorf("%s survived: %v", bad, out)
		}
	}
	for _, want := range []string{"PATH=/bin", "HOME=/h", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1"} {
		if !strings.Contains(joined, "\n"+want+"\n") {
			t.Errorf("missing %s: %v", want, out)
		}
	}
	if gitEnv(nil) == nil {
		t.Error("must return a non-nil slice")
	}
}

func TestGitEnvFoldsCaseWhereTheOSDoes(t *testing.T) {
	in := []string{"Git_Dir=/x", "herdr_pane_id=w1:p1", "Path=/bin"}
	out := gitEnv(in)
	joined := strings.Join(out, "\n")
	if platform.EnvKey("a") == "A" { // Windows: Git_Dir is GIT_DIR
		if strings.Contains(joined, "Git_Dir") || strings.Contains(joined, "herdr_pane_id") || !strings.Contains(joined, "Path=/bin") {
			t.Fatalf("Windows gitEnv = %v", out)
		}
		return
	}
	if !strings.Contains(joined, "Git_Dir=/x") || !strings.Contains(joined, "herdr_pane_id") {
		t.Fatalf("Unix gitEnv = %v", out)
	}
}
