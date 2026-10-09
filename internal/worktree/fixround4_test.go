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

// P7-R6: drafts can't create or change symlinks. Every diff block whose new
// mode is 120000 is refused, by Validate and by Apply from the bytes alone.

// wantNoSymlinks asserts err is the symlink refusal.
func wantNoSymlinks(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, ErrEscape) || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("%s: err %v, want ErrEscape refusing the symlink", what, err)
	}
}

// linkRepo is newRepo plus a committed in-repo symlink lnk -> a.txt.
func linkRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	symlinkOrSkip(t, "a.txt", filepath.Join(repo, "lnk"))
	sh(t, repo, "add", "lnk")
	sh(t, repo, "commit", "-qm", "link")
	return repo
}

// isLinkOrSkip skips when the draft checked lnk out as a plain file.
func isLinkOrSkip(t *testing.T, p string) {
	t.Helper()
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Skip("this git checks symlinks out as plain files (core.symlinks=false)")
	}
}

func TestValidateRefusesAnHonestDraftThatCreatesASymlink(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, co, p, diff := draftT(t, g, repo, func(dir string) {
		symlinkOrSkip(t, "a.txt", filepath.Join(dir, "fine")) // in-repo, harmless target
	})
	wantNoSymlinks(t, "Validate", Validate(context.Background(), g, co, p, Caps{Deny: noDeny}))
	_, err := Apply(context.Background(), g, repo, diff)
	wantNoSymlinks(t, "Apply", err)
	if _, err := os.Lstat(filepath.Join(repo, "fine")); err == nil {
		t.Fatal("Apply created the symlink")
	}
}

func TestValidateRefusesAChainedSymlinkDraft(t *testing.T) {
	g := isoGit(t)
	_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		symlinkOrSkip(t, ".", filepath.Join(dir, "up"))
		symlinkOrSkip(t, "up/../SECRET", filepath.Join(dir, "l"))
	})
	wantNoSymlinks(t, "Validate", Validate(context.Background(), g, co, p, Caps{Deny: noDeny}))
}

func TestValidateAndApplyRefuseATypeChangeToASymlink(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, co, p, diff := draftT(t, g, repo, func(dir string) {
		if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, "a.txt", filepath.Join(dir, "b.txt"))
	})
	wantNoSymlinks(t, "Validate", Validate(context.Background(), g, co, p, Caps{Deny: noDeny}))
	_, err := Apply(context.Background(), g, repo, diff)
	wantNoSymlinks(t, "Apply", err)
	if fi, err := os.Lstat(filepath.Join(repo, "b.txt")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("b.txt must stay a regular file: %v %v", fi, err)
	}
}

func TestValidateAndApplyRefuseASymlinkRetarget(t *testing.T) {
	g := isoGit(t)
	repo := linkRepo(t)
	_, co, p, diff := draftT(t, g, repo, func(dir string) {
		lnk := filepath.Join(dir, "lnk")
		isLinkOrSkip(t, lnk)
		if err := os.Remove(lnk); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, "b.txt", lnk)
	})
	wantNoSymlinks(t, "Validate", Validate(context.Background(), g, co, p, Caps{Deny: noDeny}))
	_, err := Apply(context.Background(), g, repo, diff)
	wantNoSymlinks(t, "Apply", err)
	if target, err := os.Readlink(filepath.Join(repo, "lnk")); err != nil || target != "a.txt" {
		t.Fatalf("lnk -> %q (%v), want a.txt", target, err)
	}
}

func TestDeletingASymlinkIsAllowedAndApplies(t *testing.T) {
	g := isoGit(t)
	repo := linkRepo(t)
	_, co, p, diff := draftT(t, g, repo, func(dir string) {
		lnk := filepath.Join(dir, "lnk")
		isLinkOrSkip(t, lnk)
		if err := os.Remove(lnk); err != nil {
			t.Fatal(err)
		}
	})
	if !slices.Contains(p.Flags, FlagSymlink) {
		t.Errorf("flags %v: a deleted symlink is flagged for the card", p.Flags)
	}
	if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), g, repo, diff); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "lnk")); !os.IsNotExist(err) {
		t.Fatalf("lnk is still there: %v", err)
	}
}

func TestReplacingASymlinkWithARegularFileIsAllowedAndApplies(t *testing.T) {
	g := isoGit(t)
	repo := linkRepo(t)
	_, co, p, diff := draftT(t, g, repo, func(dir string) {
		lnk := filepath.Join(dir, "lnk")
		isLinkOrSkip(t, lnk)
		if err := os.Remove(lnk); err != nil {
			t.Fatal(err)
		}
		writeFile(t, lnk, "plain\n")
	})
	if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), g, repo, diff); err != nil {
		t.Fatal(err)
	}
	p2 := filepath.Join(repo, "lnk")
	if fi, err := os.Lstat(p2); err != nil || !fi.Mode().IsRegular() || readFile(t, p2) != "plain\n" {
		t.Fatalf("lnk must be a regular file holding plain: %v %v", fi, err)
	}
	if readFile(t, filepath.Join(repo, "a.txt")) != "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n" {
		t.Fatal("a.txt (the old target) changed")
	}
}

// forgedLink is a diff block that creates lnk -> /etc/passwd.
const forgedLink = "diff --git a/lnk b/lnk\nnew file mode 120000\nindex 0000000..e69de29\n--- /dev/null\n+++ b/lnk\n@@ -0,0 +1 @@\n+/etc/passwd\n\\ No newline at end of file\n"

func TestValidateAndApplyRefuseAForgedSymlinkFromTheDiffBytes(t *testing.T) {
	toLink := func(d string) string {
		d = strings.Replace(d, "new file mode 100644", "new file mode 120000", 1)
		return strings.Replace(d, "\n+n\n", "\n+/etc/passwd\n\\ No newline at end of file\n", 1)
	}
	asLink := func(cs []Change) []Change {
		out := append([]Change(nil), cs...)
		for i := range out {
			if out[i].Path == "new.txt" {
				out[i].NewMode = "120000"
			}
		}
		return out
	}
	keep := func(cs []Change) []Change { return cs }
	withPasswd := func(d string) string {
		return strings.Replace(d, "\n+n\n", "\n+/etc/passwd\n\\ No newline at end of file\n", 1)
	}
	type forgery struct {
		name    string
		diff    func(string) string
		changes func([]Change) []Change
		created string // the path a successful apply would create
	}
	cases := []forgery{
		{"new file mode 120000, Changes honest", toLink, keep, "new.txt"},
		{"new file mode 120000, Changes say symlink", toLink, asLink, "new.txt"},
		{"an extra block Changes don't mention", func(d string) string { return d + forgedLink }, keep, "lnk"},
		{"an extra block Changes list as a symlink", func(d string) string { return d + forgedLink }, func(cs []Change) []Change {
			return append(append([]Change(nil), cs...), Change{Path: "lnk", Status: "A", OldMode: "000000", NewMode: "120000"})
		}, "lnk"},
		// The header says 100644; a second mode line after +++ (git still
		// reads it as header) says 120000.
		{"new file mode after +++", func(d string) string {
			return strings.Replace(withPasswd(d), "+++ b/new.txt\n", "+++ b/new.txt\nnew file mode 120000\n", 1)
		}, keep, "new.txt"},
		{"new mode after +++ on an edit", func(d string) string {
			return strings.Replace(d, "+++ b/b.txt\n", "+++ b/b.txt\nnew mode 120000\n", 1)
		}, keep, ""},
		// git canonicalizes 120777 to a symlink.
		{"a non-canonical symlink mode", func(d string) string {
			return strings.Replace(toLink(d), "new file mode 120000", "new file mode 120777", 1)
		}, keep, "new.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, co, p := forged(t)
			orig := string(p.Diff)
			p.Diff = []byte(c.diff(orig))
			if string(p.Diff) == orig {
				t.Fatal("the rewrite did not change the diff")
			}
			p.Bytes = len(p.Diff)
			p.Changes = c.changes(p.Changes)
			p.Files = len(p.Changes)
			if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); !errors.Is(err, ErrEscape) {
				t.Errorf("Validate: err %v, want ErrEscape", err)
			}
			repo := newRepo(t)
			_, err := Apply(context.Background(), g, repo, p.Diff)
			if !errors.Is(err, ErrEscape) {
				t.Errorf("Apply: err %v, want ErrEscape", err)
			}
			if c.created != "" {
				if _, err := os.Lstat(filepath.Join(repo, c.created)); err == nil {
					t.Errorf("Apply created %s", c.created)
				}
			}
			if fi, err := os.Lstat(filepath.Join(repo, "b.txt")); err != nil || !fi.Mode().IsRegular() {
				t.Errorf("b.txt must stay a regular file: %v %v", fi, err)
			}
		})
	}
}

func TestQuotedPathsCaptureValidateAndApply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`Windows file names can't hold "`)
	}
	g := isoGit(t)
	quote, accent := `q"q.txt`, "é.txt"
	setup := func(t *testing.T) string {
		repo := newRepo(t)
		writeFile(t, filepath.Join(repo, quote), "q\n")
		writeFile(t, filepath.Join(repo, accent), "e\n")
		sh(t, repo, "add", ".")
		sh(t, repo, "commit", "-qm", "odd names")
		return repo
	}
	edit := func(dir string) {
		writeFile(t, filepath.Join(dir, quote), "q2\n")
		writeFile(t, filepath.Join(dir, accent), "e2\n")
	}
	check := func(t *testing.T, co Checkout, p Patch, repo string) {
		t.Helper()
		if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err != nil {
			t.Fatal(err)
		}
		if _, err := Apply(context.Background(), g, repo, p.Diff); err != nil {
			t.Fatal(err)
		}
		if readFile(t, filepath.Join(repo, quote)) != "q2\n" || readFile(t, filepath.Join(repo, accent)) != "e2\n" {
			t.Fatal("the quoted paths were not edited")
		}
	}
	t.Run("as captured", func(t *testing.T) {
		repo := setup(t)
		_, co, p, diff := draftT(t, g, repo, edit)
		if !strings.Contains(string(diff), `diff --git "a/q\"q.txt" "b/q\"q.txt"`) {
			t.Fatalf("the diff does not quote the path; the test proves nothing:\n%s", diff)
		}
		check(t, co, p, repo)
	})
	// The runner pins core.quotePath=false; a diff from a git that quotes
	// non-ASCII (the default) must work too.
	t.Run("core.quotePath=true", func(t *testing.T) {
		repo := setup(t)
		s, co, p, _ := draftT(t, g, repo, edit)
		quoted := sh(t, co.Dir, "-c", "core.quotePath=true", "diff", "--cached", "--no-renames", "--no-color",
			"--src-prefix=a/", "--dst-prefix=b/", "-U3", "--binary", s.Commit, "--")
		p.Diff = []byte(quoted + "\n")
		p.Bytes = len(p.Diff)
		if !strings.Contains(quoted, `diff --git "a/\303\251.txt" "b/\303\251.txt"`) {
			t.Fatalf("the diff does not octal-quote é; the test proves nothing:\n%s", quoted)
		}
		check(t, co, p, repo)
	})
}

func TestDiffHeadersUnquotesGitCQuoting(t *testing.T) {
	diff := "diff --git \"a/x\\ty\\\\z\\\"\\303\\251\" \"b/x\\ty\\\\z\\\"\\303\\251\"\nindex 1111111..2222222 100644\n--- \"a/x\"\n+++ \"b/x\"\n@@ -1 +1 @@\n-a\n+b\n"
	modes, renames, err := diffHeaders([]byte(diff))
	if err != nil || renames {
		t.Fatalf("err %v renames %v", err, renames)
	}
	if m, ok := modes["x\ty\\z\"é"]; !ok || m != (fileModes{"100644", "100644"}) {
		t.Fatalf("modes %q", modes)
	}
	for _, bad := range []string{
		"diff --git \"a/x\\q\" \"b/x\\q\"\nindex 1111111..2222222 100644\n",       // unknown escape
		"diff --git \"a/x\\4000\" \"b/x\\4000\"\nindex 1111111..2222222 100644\n", // octal over \377
		"diff --git \"a/x \"b/x\"\nindex 1111111..2222222 100644\n",               // unterminated
		"diff --git \"a/x\" b/x\nindex 1111111..2222222 100644\n",                 // mixed quoting
	} {
		if _, _, err := diffHeaders([]byte(bad)); !errors.Is(err, ErrEscape) {
			t.Errorf("%q: err %v, want ErrEscape", bad, err)
		}
	}
}

func TestDiffHeadersReadsModesTheWayGitApplyDoes(t *testing.T) {
	const hunk = "@@ -1 +1 @@\n-a\n+b\n"
	ok := map[string]fileModes{
		"diff --git a/l b/l\ndeleted file mode 120000\nindex 1111111..0000000\n--- a/l\n+++ /dev/null\n@@ -1 +0,0 @@\n-a.txt\n": {"120000", "000000"},
		"diff --git a/x b/x\nindex 1111111..2222222 100644\n--- a/x\n+++ b/x\n" + hunk:                                          {"100644", "100644"},
		"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n":                                                                {"100644", "100755"},
	}
	for diff, want := range ok {
		modes, _, err := diffHeaders([]byte(diff))
		if err != nil || len(modes) != 1 {
			t.Errorf("%q: %v %v", diff, modes, err)
			continue
		}
		for _, m := range modes {
			if m != want {
				t.Errorf("%q: %v, want %v", diff, m, want)
			}
		}
	}
	for name, diff := range map[string]string{
		"a mode line in the content":   "diff --git a/x b/x\nindex 1111111..2222222 100644\n--- a/x\n+++ b/x\n" + hunk + "new file mode 120000\n",
		"a header line before a block": "new mode 120000\ndiff --git a/x b/x\nindex 1111111..2222222 100644\n",
		"half a mode change":           "diff --git a/x b/x\nold mode 100644\n--- a/x\n+++ b/x\n" + hunk,
		"index mode plus mode lines":   "diff --git a/x b/x\nold mode 100644\nnew mode 100755\nindex 1111111..2222222 100644\n",
		"index line with junk":         "diff --git a/x b/x\nindex 1111111..2222222 100644 junk\n",
		"symlink via the index line":   "diff --git a/x b/x\nindex 1111111..2222222 120000\n--- a/x\n+++ b/x\n" + hunk,
		"symlink via new mode":         "diff --git a/x b/x\nold mode 100644\nnew mode 120000\n",
		"a CR after the mode":          "diff --git a/x b/x\nnew file mode 120000\r\n",
		"a gitlink spelled oddly":      "diff --git a/x b/x\nnew file mode 777\n",
	} {
		if _, _, err := diffHeaders([]byte(diff)); !errors.Is(err, ErrEscape) {
			t.Errorf("%s: err %v, want ErrEscape", name, err)
		}
	}
}
