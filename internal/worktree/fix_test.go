package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// lockingGit wraps git so `worktree add` registers the worktree locked
// ("initializing" in real life) and then stalls past the timeout.
func lockingGit(t *testing.T) Git {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	wrapper := filepath.Join(t.TempDir(), "lockgit")
	writeFile(t, wrapper, "#!/bin/sh\ncase \"$*\" in *\" worktree add \"*) git \"$@\" --lock; sleep 10; exit 1;; esac\nexec git \"$@\"\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	g := isoGit(t)
	g.Bin, g.Timeout = wrapper, 3*time.Second
	return g
}

func noWorktrees(t *testing.T, repo string) {
	t.Helper()
	if list := sh(t, repo, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("git still lists draft worktrees:\n%s", list)
	}
	if ents, _ := os.ReadDir(filepath.Join(repo, ".git", "worktrees")); len(ents) != 0 {
		t.Fatalf(".git/worktrees not clean: %v", ents)
	}
}

func TestAddThatTimesOutCleansUpItsLockedRegistration(t *testing.T) {
	g := lockingGit(t)
	repo := newRepo(t)
	s := snapshotT(t, isoGit(t), repo)
	dir := filepath.Join(t.TempDir(), "worktrees", s.ID)
	if _, err := Add(context.Background(), g, s, dir); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Add = %v, want ErrTimeout", err)
	}
	noWorktrees(t, repo)
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir left behind: %v", err)
	}
}

func TestPruneRemovesLockedRegistrationsOfMissingDrafts(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	wts := filepath.Join(t.TempDir(), "worktrees")
	id := NewID()
	dir := filepath.Join(wts, id)
	sh(t, repo, "worktree", "add", "--detach", "--lock", "--reason", "initializing", dir, "HEAD")
	other := filepath.Join(t.TempDir(), "user-wt")
	sh(t, repo, "worktree", "add", "--detach", "--lock", other, "HEAD")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(context.Background(), g, repo, wts, time.Hour, nil); err != nil {
		t.Fatal(err)
	}
	list := sh(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, id) {
		t.Fatalf("locked draft registration survived Prune:\n%s", list)
	}
	if !strings.Contains(list, filepath.Base(other)) {
		t.Fatalf("Prune touched a user's own worktree:\n%s", list)
	}
}

func TestPruneUnlocksAndRemovesAnOldLockedDraftDirectory(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	wts := filepath.Join(t.TempDir(), "worktrees")
	dir := filepath.Join(wts, NewID())
	sh(t, repo, "worktree", "add", "--detach", "--lock", dir, "HEAD")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	if n, err := Prune(context.Background(), g, repo, wts, time.Hour, nil); err != nil || n != 1 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	noWorktrees(t, repo)
}

// filterMarker configures a filter named evil whose commands leave a marker.
func filterMarker(t *testing.T, repo string) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "filter-ran")
	cmd := "echo ran >> '" + filepath.ToSlash(marker) + "'; cat"
	for _, k := range []string{"clean", "smudge"} {
		sh(t, repo, "config", "filter.evil."+k, cmd)
	}
	return marker
}

func TestWorktreeSideCommandsNeverRunFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell filter")
	}
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, ".gitattributes"), "*.txt filter=evil\n")
	sh(t, repo, "add", ".gitattributes")
	sh(t, repo, "commit", "-qm", "attrs")
	s := snapshotT(t, g, repo) // the filter is configured only after the snapshot
	marker := filterMarker(t, repo)
	co := checkoutT(t, g, s)
	writeFile(t, filepath.Join(co.Dir, "a.txt"), "edited\n")
	for _, args := range [][]string{{"status", "--porcelain"}, {"add", "-A"}, {"diff", "--cached", "--binary"}} {
		if _, err := co.Git(context.Background(), args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a filter ran in the draft: %s", b)
	}
}

func TestSnapshotKeepsTheUsersCleanFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses tr as the clean filter")
	}
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, ".gitattributes"), "b.txt filter=up\n")
	sh(t, repo, "add", ".gitattributes")
	sh(t, repo, "commit", "-qm", "attrs")
	sh(t, repo, "config", "filter.up.clean", "tr a-z A-Z")
	writeFile(t, filepath.Join(repo, "b.txt"), "bee\n")
	s := snapshotT(t, g, repo)
	if got := sh(t, repo, "show", s.Commit+":b.txt"); got != "BEE" {
		t.Fatalf("b.txt in snapshot = %q, want the filtered blob", got)
	}
}

func TestCheckoutGitRefusesAForgedDotGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell filter")
	}
	g := isoGit(t)
	repo := newRepo(t)
	s := snapshotT(t, g, repo)
	co := checkoutT(t, g, s)
	forged := newRepo(t)
	marker := filterMarker(t, forged)
	writeFile(t, filepath.Join(forged, ".git", "info", "attributes"), "* filter=evil\n")
	writeFile(t, filepath.Join(co.Dir, ".git"), "gitdir: "+filepath.ToSlash(filepath.Join(forged, ".git"))+"\n")
	writeFile(t, filepath.Join(co.Dir, "x.txt"), "x\n")
	if _, err := co.Git(context.Background(), "add", "-A"); err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("err %v, want a refusal naming .git", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the forged repository's filter ran")
	}
}

func TestCancelledContextIsNotReportedAsNotGit(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := TakeSnapshot(ctx, g, repo, NewID(), 0); !errors.Is(err, context.Canceled) || errors.Is(err, ErrNotGit) || errors.Is(err, ErrUnbornHead) {
		t.Fatalf("err %v, want context.Canceled", err)
	}
}

func TestEscapesRefusesNTFSAliasesOfDotGit(t *testing.T) {
	for _, target := range []string{".git./config", ".git /x", "GIT~1/x", "sub/.git/hooks"} {
		if !Escapes("link", target) {
			t.Errorf("Escapes(link, %q) = false", target)
		}
	}
}

func TestRestoreDefangedNeverFollowsAParentSymlink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	symlinkOrSkip(t, outside, filepath.Join(dir, "d"))
	co := Checkout{Dir: dir, Defanged: map[string]string{"d/link": "/etc"}}
	if _, err := restoreDefanged(co); err == nil {
		t.Fatal("restore must refuse a parent that is not a directory")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("restore wrote outside the checkout: %v", ents)
	}
}
