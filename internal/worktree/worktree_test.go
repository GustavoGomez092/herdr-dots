package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/platform/platformtest"
)

// userState is everything a snapshot must leave exactly as it was.
type userState struct {
	index, stash, head, refs, status string
}

func stateOf(t *testing.T, repo string) userState {
	t.Helper()
	idx, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	return userState{
		index:  string(idx),
		stash:  sh(t, repo, "stash", "list"),
		head:   sh(t, repo, "rev-parse", "HEAD"),
		refs:   sh(t, repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags", "refs/stash"),
		status: sh(t, repo, "status", "--porcelain=v1", "--untracked-files=all"),
	}
}

func TestSnapshotCapturesTheWorkingTreeAndLeavesTheUserStateAlone(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\n") // unstaged
	writeFile(t, filepath.Join(repo, "b.txt"), "b staged\n")
	sh(t, repo, "add", "b.txt")
	writeFile(t, filepath.Join(repo, "b.txt"), "b staged then edited\n") // staged v2, working v3
	writeFile(t, filepath.Join(repo, "s.txt"), "staged new\n")
	sh(t, repo, "add", "s.txt")
	writeFile(t, filepath.Join(repo, "u.txt"), "untracked\n")
	writeFile(t, filepath.Join(repo, "debug.log"), "ignored\n")
	writeFile(t, filepath.Join(repo, "build", "out.bin"), "ignored\n")
	if err := os.Remove(filepath.Join(repo, "c.txt")); err != nil {
		t.Fatal(err)
	}
	before := stateOf(t, repo)

	s, err := TakeSnapshot(context.Background(), g, filepath.Join(repo), NewID(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if after := stateOf(t, repo); after != before {
		t.Fatalf("user state changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if s.BaseHead != before.head || sh(t, repo, "rev-parse", s.Commit+"^") != before.head {
		t.Fatalf("snapshot %+v is not parented on HEAD %s", s, before.head)
	}
	if got := sh(t, repo, "rev-parse", s.Ref); got != s.Commit {
		t.Fatalf("ref %s = %s, want %s", s.Ref, got, s.Commit)
	}
	files := strings.Fields(sh(t, repo, "ls-tree", "-r", "--name-only", s.Commit))
	slices.Sort(files)
	if want := []string{".gitignore", "a.txt", "b.txt", "s.txt", "u.txt"}; !slices.Equal(files, want) {
		t.Fatalf("snapshot files %v, want %v", files, want)
	}
	for path, want := range map[string]string{"a.txt": "1\nTWO\n3", "b.txt": "b staged then edited", "u.txt": "untracked"} {
		if got := sh(t, repo, "show", s.Commit+":"+path); !strings.HasPrefix(got, want) {
			t.Errorf("%s in snapshot = %q, want prefix %q", path, got, want)
		}
	}
}

func TestSnapshotRefusesANonRepoAndAnUnbornHead(t *testing.T) {
	g := isoGit(t)
	if _, err := TakeSnapshot(context.Background(), g, t.TempDir(), NewID(), 0); !errors.Is(err, ErrNotGit) {
		t.Fatalf("plain dir: %v, want ErrNotGit", err)
	}
	empty := t.TempDir()
	sh(t, empty, "init", "-q")
	writeFile(t, filepath.Join(empty, "x.txt"), "x\n")
	if _, err := TakeSnapshot(context.Background(), g, empty, NewID(), 0); !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("no commits: %v, want ErrUnbornHead", err)
	}
	if _, err := TakeSnapshot(context.Background(), g, newRepo(t), "../../x", 0); err == nil {
		t.Fatal("an invalid id must be refused")
	}
}

func TestSnapshotOverTheIndexCapIsTooLarge(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	if _, err := TakeSnapshot(context.Background(), g, repo, NewID(), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err %v, want ErrTooLarge", err)
	}
	if ids, _ := SnapshotIDs(context.Background(), g, repo); len(ids) != 0 {
		t.Fatalf("a refused snapshot left refs %v", ids)
	}
}

func TestSlowSnapshotIsTooLarge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	wrapper := filepath.Join(t.TempDir(), "slowgit")
	writeFile(t, wrapper, "#!/bin/sh\ncase \"$*\" in *\" add -A \"*) sleep 10;; esac\nexec git \"$@\"\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	g := isoGit(t)
	g.Bin, g.Timeout = wrapper, 3*time.Second
	_, err := TakeSnapshot(context.Background(), g, newRepo(t), NewID(), 0)
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "commit or ignore build output") {
		t.Fatalf("err %v, want ErrTooLarge with a hint", err)
	}
}

func TestHooksNeverRunDuringSnapshotCheckoutAndRemove(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	marker := installMarkerHooks(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "changed\n")
	s := snapshotT(t, g, repo)
	co, err := Add(context.Background(), g, s, filepath.Join(t.TempDir(), "worktrees", s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(context.Background(), g, repo, co.Dir); err != nil {
		t.Fatal(err)
	}
	if err := DropSnapshot(context.Background(), g, repo, s.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a hook ran: %s", b)
	}
}

// V5: a repository's core.fsmonitor may name a program; it must never run.
func TestAnFsmonitorProgramNeverRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fsmonitor program")
	}
	g := isoGit(t)
	repo := newRepo(t)
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	script := filepath.Join(t.TempDir(), "fsmonitor.sh")
	writeFile(t, script, "#!/bin/sh\necho ran >> '"+marker+"'\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	sh(t, repo, "config", "core.fsmonitor", script)
	writeFile(t, filepath.Join(repo, "a.txt"), "changed\n")
	s := snapshotT(t, g, repo)
	checkoutT(t, g, s)
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("the fsmonitor program ran: %s", b)
	}
}

// V5: a sparse checkout snapshots what is checked out, and files outside
// the sparse cone stay in the snapshot as they are in HEAD.
func TestSnapshotOfASparseCheckout(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "docs", "guide.md"), "guide\n")
	sh(t, repo, "add", ".")
	sh(t, repo, "commit", "-qm", "docs")
	sh(t, repo, "sparse-checkout", "set", "--no-cone", "/*.txt")
	if _, err := os.Stat(filepath.Join(repo, "docs", "guide.md")); !os.IsNotExist(err) {
		t.Skip("this git did not apply the sparse pattern")
	}
	writeFile(t, filepath.Join(repo, "a.txt"), "sparse edit\n")
	before := stateOf(t, repo)
	s := snapshotT(t, g, repo)
	if after := stateOf(t, repo); after != before {
		t.Fatalf("user state changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if got := sh(t, repo, "show", s.Commit+":a.txt"); got != "sparse edit" {
		t.Fatalf("a.txt in snapshot = %q", got)
	}
	if got := sh(t, repo, "show", s.Commit+":docs/guide.md"); got != "guide" {
		t.Fatalf("a file outside the sparse cone must stay in the snapshot, got %q", got)
	}
}

func TestAddChecksOutTheSnapshotAndDefangsEscapingSymlinks(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "outside\n")
	symlinkOrSkip(t, outside, filepath.Join(repo, "out"))
	symlinkOrSkip(t, "a.txt", filepath.Join(repo, "in"))
	symlinkOrSkip(t, "../"+filepath.Base(repo)+"-x", filepath.Join(repo, "up"))
	writeFile(t, filepath.Join(repo, "a.txt"), "uncommitted\n")
	s := snapshotT(t, g, repo)
	co := checkoutT(t, g, s)
	if fi, err := os.Lstat(filepath.Join(co.Dir, "in")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Skip("this git checks symlinks out as plain files (core.symlinks=false)")
	}
	if got := readFile(t, filepath.Join(co.Dir, "a.txt")); got != "uncommitted\n" {
		t.Fatalf("worktree a.txt = %q, want the uncommitted content", got)
	}
	for _, name := range []string{"out", "up"} {
		fi, err := os.Lstat(filepath.Join(co.Dir, name))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s must be defanged: %v %v", name, fi, err)
		}
		if readFile(t, filepath.Join(co.Dir, name)) != DefangNote {
			t.Fatalf("%s stand-in content", name)
		}
	}
	if fi, err := os.Lstat(filepath.Join(co.Dir, "in")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("an in-repo symlink stays a symlink: %v %v", fi, err)
	}
	if len(co.Defanged) != 2 || co.Defanged["out"] != outside {
		t.Fatalf("defanged %v", co.Defanged)
	}
}

func TestEscapes(t *testing.T) {
	for _, c := range []struct {
		rel, target string
		want        bool
	}{
		{"link", "a.txt", false},
		{"dir/link", "../a.txt", false},
		{"dir/link", "../../a.txt", true},
		{"link", "/etc/passwd", true},
		{"link", "..", true},
		{"link", ".git/config", true},
		{"dir/link", "../.GIT", true},
		{"link", "", true},
		{"link", `C:\Windows`, true},
	} {
		if got := Escapes(c.rel, c.target); got != c.want {
			t.Errorf("Escapes(%q, %q) = %v, want %v", c.rel, c.target, got, c.want)
		}
	}
}

func TestCheckPath(t *testing.T) {
	for _, ok := range []string{"a.txt", "src/app/main.go", ".gitignore", ".github/workflows/ci.yml", "a/b.c/d"} {
		if err := CheckPath(ok); err != nil {
			t.Errorf("CheckPath(%q) = %v", ok, err)
		}
	}
	for path, want := range map[string]error{
		".git": ErrGitDir, ".git/config": ErrGitDir, "x/.Git/HEAD": ErrGitDir, ".git /x": ErrGitDir, "git~1/x": ErrGitDir,
		"": ErrEscape, "../x": ErrEscape, "a/./b": ErrEscape, "a//b": ErrEscape, "/abs": ErrEscape, "C:/x": ErrEscape,
		`a\b`: ErrEscape, "a\nb": ErrEscape, "a:b": ErrEscape,
	} {
		if err := CheckPath(path); !errors.Is(err, want) {
			t.Errorf("CheckPath(%q) = %v, want %v", path, err, want)
		}
	}
	// Windows-reserved names follow platform.ReservedPathName (Phase 6 made it
	// per-OS): refused on Windows, ordinary names on Unix.
	for _, name := range []string{"aux", "src/nul.txt", "a.txt:secret", "env."} {
		err := CheckPath(name)
		if refused := errors.Is(err, ErrEscape); refused != platform.ReservedPathName(name) {
			t.Errorf("CheckPath(%q) = %v, want refused=%v (platform.ReservedPathName)", name, err, platform.ReservedPathName(name))
		}
	}
}

func TestRemoveAndPruneLeaveNoWorktreesBehind(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	wts := filepath.Join(t.TempDir(), "worktrees")
	add := func() Checkout {
		s := snapshotT(t, g, repo)
		co, err := Add(context.Background(), g, s, filepath.Join(wts, s.ID))
		if err != nil {
			t.Fatal(err)
		}
		return co
	}
	if err := os.MkdirAll(wts, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := add()
	platformtest.AssertPrivateDir(t, wts) // the checkouts' parent is made private
	if err := Remove(context.Background(), g, repo, gone.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(gone.Dir); !os.IsNotExist(err) {
		t.Fatalf("removed worktree dir still there: %v", err)
	}
	crashed, live, young := add(), add(), add()
	old := time.Now().Add(-2 * time.Hour)
	for _, co := range []Checkout{crashed, live} {
		if err := os.Chtimes(co.Dir, old, old); err != nil {
			t.Fatal(err)
		}
	}
	n, err := Prune(context.Background(), g, repo, wts, time.Hour, map[string]bool{filepath.Base(live.Dir): true})
	if err != nil || n != 1 {
		t.Fatalf("Prune = %d, %v; want 1 removed", n, err)
	}
	if _, err := os.Lstat(crashed.Dir); !os.IsNotExist(err) {
		t.Fatal("the orphaned worktree survived Prune")
	}
	list := sh(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, filepath.Base(crashed.Dir)) || strings.Contains(list, filepath.Base(gone.Dir)) {
		t.Fatalf("git still lists a pruned worktree:\n%s", list)
	}
	for _, co := range []Checkout{live, young} {
		if !strings.Contains(list, filepath.Base(co.Dir)) {
			t.Fatalf("Prune removed a live or young worktree %s:\n%s", co.Dir, list)
		}
	}
}

func TestDropSnapshotIsIdempotentAndSnapshotIDsListsRefs(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	a, b := snapshotT(t, g, repo), snapshotT(t, g, repo)
	ids, err := SnapshotIDs(context.Background(), g, repo)
	slices.Sort(ids)
	want := []string{a.ID, b.ID}
	slices.Sort(want)
	if err != nil || !slices.Equal(ids, want) {
		t.Fatalf("SnapshotIDs = %v, %v; want %v", ids, err, want)
	}
	for range 2 {
		if err := DropSnapshot(context.Background(), g, repo, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if ids, _ := SnapshotIDs(context.Background(), g, repo); !slices.Equal(ids, []string{b.ID}) {
		t.Fatalf("after drop: %v", ids)
	}
	if err := DropSnapshot(context.Background(), g, repo, "../HEAD"); err == nil {
		t.Fatal("an invalid id must be refused")
	}
}

func TestGitEnvDropsRedirectsAndPinsSafety(t *testing.T) {
	out := gitEnv([]string{"PATH=/bin", "GIT_DIR=/x", "GIT_INDEX_FILE=/y", "GIT_CONFIG_GLOBAL=/z", "HERDR_SOCKET_PATH=/s", "CLAUDECODE=1", "LANG=de_DE"})
	for _, bad := range []string{"GIT_DIR=/x", "GIT_INDEX_FILE=/y", "GIT_CONFIG_GLOBAL=/z", "HERDR_SOCKET_PATH=/s", "CLAUDECODE=1", "LANG=de_DE"} {
		if slices.Contains(out, bad) {
			t.Errorf("kept %s", bad)
		}
	}
	for _, want := range []string{"PATH=/bin", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_LFS_SKIP_SMUDGE=1", "GIT_NO_LAZY_FETCH=1", "LC_ALL=C"} {
		if !slices.Contains(out, want) {
			t.Errorf("missing %s in %v", want, out)
		}
	}
}

func TestValidIDAndNewID(t *testing.T) {
	if id := NewID(); !ValidID(id) {
		t.Fatalf("NewID %q is not valid", id)
	}
	for _, bad := range []string{"", "../x", "ABCDEF0123456789", "0123456789abcdef0", "0123456789abcdeg"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true", bad)
		}
	}
}
