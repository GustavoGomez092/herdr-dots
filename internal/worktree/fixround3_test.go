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
)

// forged returns a real captured patch for a draft that edits b.txt and adds
// new.txt, so a test can rewrite its Diff while Changes stay honest.
func forged(t *testing.T) (Git, Checkout, Patch) {
	t.Helper()
	g := isoGit(t)
	_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b2\n")
		writeFile(t, filepath.Join(dir, "new.txt"), "n\n")
	})
	if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err != nil {
		t.Fatalf("the honest patch must validate: %v", err)
	}
	return g, co, p
}

func TestValidateRefusesModesTheDiffCarriesButChangesDoNotMention(t *testing.T) {
	for name, rewrite := range map[string]func(string) string{
		"symlink in a new file":  func(d string) string { return strings.Replace(d, "new file mode 100644", "new file mode 120000", 1) },
		"gitlink in a new file":  func(d string) string { return strings.Replace(d, "new file mode 100644", "new file mode 160000", 1) },
		"symlink via index line": func(d string) string { return strings.Replace(d, "100644\n--- a/b.txt", "120000\n--- a/b.txt", 1) },
		"mode change header":     func(d string) string { return strings.Replace(d, "new file mode 100644", "new file mode 100755", 1) },
	} {
		g, co, p := forged(t)
		orig := string(p.Diff)
		p.Diff = []byte(rewrite(orig))
		if string(p.Diff) == orig {
			t.Fatalf("%s: the rewrite did not change the diff", name)
		}
		if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); !errors.Is(err, ErrEscape) {
			t.Errorf("%s: err %v, want ErrEscape", name, err)
		}
	}
}

func TestValidateRefusesARenameOrCopyThatHidesASourcePath(t *testing.T) {
	g, co, _ := forged(t)
	for _, verb := range []string{"rename", "copy"} {
		diff := "diff --git a/.env b/x.txt\nsimilarity index 100%\n" + verb + " from .env\n" + verb + " to x.txt\n"
		p := Patch{Diff: []byte(diff), Files: 1, Bytes: len(diff),
			Changes: []Change{{Path: "x.txt", Status: "A", OldMode: "000000", NewMode: "100644"}}}
		if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); !errors.Is(err, ErrEscape) {
			t.Errorf("%s: err %v, want ErrEscape", verb, err)
		}
	}
}

func TestCaptureTakesOneImmutableStateEvenIfTheIndexChangesMidCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	g := isoGit(t)
	repo := newRepo(t)
	flag := filepath.Join(t.TempDir(), "mutated")
	wrapper := filepath.Join(t.TempDir(), "racegit")
	// After the first write-tree or diff in a draft, a "lingering process"
	// adds a symlink to the draft's index.
	writeFile(t, wrapper, "#!/bin/sh\ngit \"$@\"; rc=$?\n"+
		"case \"$*\" in *\" write-tree\"*|*\" diff \"*) if [ -n \"$GIT_WORK_TREE\" ] && [ ! -e '"+filepath.ToSlash(flag)+"' ]; then\n"+
		"touch '"+filepath.ToSlash(flag)+"'; (cd \"$GIT_WORK_TREE\" && ln -s /etc/passwd evil && git add evil); fi;; esac\nexit $rc\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	g.Bin = wrapper
	s := snapshotT(t, g, repo)
	co := checkoutT(t, g, s)
	writeFile(t, filepath.Join(co.Dir, "b.txt"), "b2\n")
	p, err := Capture(context.Background(), g, co, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Fatal("the wrapper never mutated the index; the test proves nothing")
	}
	if strings.Contains(string(p.Diff), "evil") || !slices.Equal(p.Paths(), []string{"b.txt"}) {
		t.Fatalf("an inconsistent patch: paths %v\n%s", p.Paths(), p.Diff)
	}
	if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err != nil {
		t.Fatal(err)
	}
}

func TestPatchPathsCannotDiscoverAnUnrelatedRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	other := newRepo(t)
	tmp := filepath.Join(other, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	g, co, p := forged(t)
	seen := filepath.Join(t.TempDir(), "seen")
	wrapper := filepath.Join(t.TempDir(), "spygit")
	writeFile(t, wrapper, "#!/bin/sh\ncase \"$*\" in *\" apply --numstat\"*) git -C \"$2\" rev-parse --is-inside-work-tree >> '"+filepath.ToSlash(seen)+"' 2>&1;; esac\nexec git \"$@\"\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	spy := g
	spy.Bin = wrapper
	if err := Validate(context.Background(), spy, co, p, Caps{Deny: noDeny}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, seen); strings.Contains(got, "true") {
		t.Fatalf("git apply --numstat could see a repository around its temp dir: %q", got)
	}
}

func TestApplyAndValidateRefuseAnEmptyPathSet(t *testing.T) {
	g, co, p := forged(t)
	p.Diff = nil
	if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err == nil {
		t.Fatal("Validate accepted an empty diff")
	}
	if _, err := Apply(context.Background(), g, newRepo(t), []byte("\n")); err == nil {
		t.Fatal("Apply accepted a diff with no files")
	}
}

func TestApplyUsesTheUsersFiltersEvenWithADraftRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell filter")
	}
	g := isoGit(t)
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, ".gitattributes"), "*.txt filter=mine\n")
	sh(t, repo, "add", ".gitattributes")
	sh(t, repo, "commit", "-qm", "attrs")
	_, _, _, diff := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b2\n")
	})
	marker := filterMarker(t, repo) // sets filter.evil.*; add the same under "mine"
	for _, k := range []string{"clean", "smudge"} {
		sh(t, repo, "config", "filter.mine."+k, "sh -c 'echo ran >> "+filepath.ToSlash(marker)+"; cat'")
	}
	draftRunner, err := g.WithoutFilters(context.Background(), repo)
	if err != nil || !draftRunner.Draft || len(draftRunner.NoFilters) == 0 {
		t.Fatalf("setup: %+v %v", draftRunner, err)
	}
	if _, err := Apply(context.Background(), draftRunner, repo, diff); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the user's filter did not run during Apply")
	}
}
