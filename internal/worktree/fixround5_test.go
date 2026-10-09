package worktree

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Fix round 5: a committed symlink (lnk -> a.txt) must not be retargeted by
// a diff our parser reads differently from git (a loose index line, an
// old-style block without "diff --git"). Apply and Validate check the
// filesystem: a path that is a symlink now may only be deleted or replaced
// by a regular file.

// symlinksIn lists every symlink under repo (outside .git) as "rel->target".
func symlinksIn(t *testing.T, repo string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			tg, _ := os.Readlink(p)
			rel, _ := filepath.Rel(repo, p)
			out = append(out, filepath.ToSlash(rel)+"->"+tg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const (
	lnkHunk     = "@@ -1 +1 @@\n-a.txt\n\\ No newline at end of file\n+/etc/passwd\n\\ No newline at end of file\n"
	newLinkHunk = "@@ -0,0 +1 @@\n+/etc/passwd\n\\ No newline at end of file\n"
	bHunk       = "@@ -1 +1 @@\n-b\n+bb\n"
	bBlock      = "diff --git a/b.txt b/b.txt\nindex 6178079..1111111 100644\n--- a/b.txt\n+++ b/b.txt\n" + bHunk
)

func change(path, status, old, new string) Change {
	return Change{Path: path, Status: status, NewSHA: strings.Repeat("1", 40), OldMode: old, NewMode: new, Added: 1, Deleted: 1}
}

// linkDraft is a draft of linkRepo whose worktree holds lnk as a symlink.
func linkDraft(t *testing.T, g Git) Checkout {
	t.Helper()
	_, co, _, _ := draftT(t, g, linkRepo(t), func(dir string) {
		isLinkOrSkip(t, filepath.Join(dir, "lnk"))
		writeFile(t, filepath.Join(dir, "b.txt"), "bb\n")
	})
	return co
}

func TestACommittedSymlinkCannotBeRetargetedByAForgedDiff(t *testing.T) {
	M := func(p string) []Change { return []Change{change(p, "M", "100644", "100644")} }
	A := func(p string) []Change { return []Change{change(p, "A", "000000", "100644")} }
	retarget := func(index string) string {
		return "diff --git a/lnk b/lnk\n" + index + "\n--- a/lnk\n+++ b/lnk\n" + lnkHunk
	}
	cases := []struct {
		name    string
		diff    string
		changes []Change
		escape  bool // must be ErrEscape (the round-5 holes); others need only fail
	}{
		{"index: tab before the mode", retarget("index 2e65efe..f4d5ecc\t100644"), M("lnk"), true},
		{"index: VT before the mode", retarget("index 2e65efe..f4d5ecc\v100644"), M("lnk"), true},
		{"index: NBSP before the mode", retarget("index 2e65efe..f4d5ecc 100644"), M("lnk"), true},
		{"index: no dots", retarget("index 2e65efe 100644"), M("lnk"), true},
		{"index: a 70-char hash", retarget("index " + strings.Repeat("a", 70) + "..f4d5ecc 100644"), M("lnk"), true},
		{"index: one dot", retarget("index 2e65efe.f4d5ecc 100644"), M("lnk"), true},
		{"old-style block after a git block", bBlock + "--- a/lnk\n+++ b/lnk\n" + lnkHunk, M("b.txt"), true},
		{"old-style block before a git block", "--- a/lnk\n+++ b/lnk\n" + lnkHunk + bBlock, M("b.txt"), true},
		// Not a symlink: only the block count sees it.
		{"old-style block on a regular file", bBlock + "--- a/c.txt\n+++ b/c.txt\n@@ -1 +1 @@\n-c\n+cc\n", M("b.txt"), true},
		{"index: honest-looking 100644 on a symlink", retarget("index 2e65efe..f4d5ecc 100644"), M("lnk"), true},
		{"old mode 100644 new mode 100755 on a symlink", "diff --git a/lnk b/lnk\nold mode 100644\nnew mode 100755\n--- a/lnk\n+++ b/lnk\n" + lnkHunk, []Change{change("lnk", "M", "100644", "100755")}, true},
		// The rest of the reviewer's probe matrix: each must fail, and no
		// symlink may land.
		{"mode 0120000", "diff --git a/n b/n\nnew file mode 0120000\n--- /dev/null\n+++ b/n\n" + newLinkHunk, A("n"), false},
		{"mode 120777", "diff --git a/n b/n\nnew file mode 120777\n--- /dev/null\n+++ b/n\n" + newLinkHunk, A("n"), false},
		{"mode 00000120000", "diff --git a/n b/n\nnew file mode 00000120000\n--- /dev/null\n+++ b/n\n" + newLinkHunk, A("n"), false},
		{"mode after two spaces", "diff --git a/n b/n\nnew file mode  120000\n--- /dev/null\n+++ b/n\n" + newLinkHunk, A("n"), false},
		{"mode with a trailing space", "diff --git a/n b/n\nnew file mode 120000 \n--- /dev/null\n+++ b/n\n" + newLinkHunk, A("n"), false},
		{"mode after a tab", "diff --git a/n b/n\nnew file mode\t120000\n--- /dev/null\n+++ b/n\n" + newLinkHunk, A("n"), false},
		{"CRLF", strings.ReplaceAll("diff --git a/n b/n\nnew file mode 120000\n--- /dev/null\n+++ b/n\n"+newLinkHunk, "\n", "\r\n"), A("n"), false},
		{"new mode after +++", "diff --git a/n b/n\nnew file mode 100644\n--- /dev/null\n+++ b/n\nnew mode 120000\n" + newLinkHunk, A("n"), false},
		{"new mode after the hunk", bBlock + "new mode 120000\n", M("b.txt"), false},
		{"two headers for one path", "diff --git a/n b/n\nnew file mode 100644\n--- /dev/null\n+++ b/n\n@@ -0,0 +1 @@\n+x\ndiff --git a/n b/n\nold mode 100644\nnew mode 120000\n", A("n"), false},
		{"delete then recreate as a symlink", "diff --git a/b.txt b/b.txt\ndeleted file mode 100644\n--- a/b.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-b\ndiff --git a/b.txt b/b.txt\nnew file mode 120000\n--- /dev/null\n+++ b/b.txt\n" + newLinkHunk, []Change{change("b.txt", "T", "100644", "100644")}, false},
		{"a binary symlink", "diff --git a/n b/n\nnew file mode 120000\nindex 0000000..1111111\nGIT binary patch\nliteral 11\nScmZp0Xmwr(00000001\n\nliteral 0\nHcmV?d00001\n\n", A("n"), false},
		{"diff --cc", "diff --cc lnk\nindex a,b..c\n--- a/lnk\n+++ b/lnk\n@@@ -1,1 -1,1 +1,1 @@@\n- a.txt\n -a.txt\n++/etc/passwd\n", M("lnk"), false},
		{"a copy header", "diff --git a/a.txt b/n\nsimilarity index 100%\ncopy from a.txt\ncopy to n\n", A("n"), false},
		{"names overridden by ---/+++", "diff --git a/b.txt b/b.txt\nindex 6178079..1111111 100644\n--- a/lnk\n+++ b/lnk\n" + lnkHunk, M("b.txt"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := isoGit(t)
			co := linkDraft(t, g)
			p := Patch{Diff: []byte(c.diff), Changes: c.changes, Files: len(c.changes), Bytes: len(c.diff)}
			verr := Validate(context.Background(), g, co, p, Caps{Deny: noDeny})
			repo := linkRepo(t)
			_, aerr := Apply(context.Background(), g, repo, []byte(c.diff))
			if verr == nil || aerr == nil || (c.escape && (!errors.Is(verr, ErrEscape) || !errors.Is(aerr, ErrEscape))) {
				t.Errorf("Validate: %v\nApply: %v\nwant both refused (ErrEscape: %v)", verr, aerr, c.escape)
			}
			if links := symlinksIn(t, repo); !slices.Equal(links, []string{"lnk->a.txt"}) {
				t.Errorf("symlinks after Apply: %v", links)
			}
		})
	}
}

func TestHonestDraftShapesStillValidateAndApply(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"edit, add, delete, chmod, binary, empty, quoted, header-like content": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "a.txt"), "1\n2\n3\n4\n5\n6\n7\n8\n9\n")
			writeFile(t, filepath.Join(dir, "img.bin"), "\x00\x01\x02bin")
			writeFile(t, filepath.Join(dir, "empty.txt"), "")
			writeFile(t, filepath.Join(dir, "sql.sql"), "+++ added\nindex x\nnew mode 120000\n--- x\n")
			if err := os.Remove(filepath.Join(dir, "c.txt")); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				writeFile(t, filepath.Join(dir, "new dir", "q\"q é.txt"), "x\n")
				if err := os.Chmod(filepath.Join(dir, "b.txt"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		},
		"remove a -- line, no newline at the end": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "a.txt"), "1\n2\n3\n4\n5\n6\n7\n8\n9\n10")
			writeFile(t, filepath.Join(dir, "dd.txt"), "keep\n")
		},
		"chmod only": func(t *testing.T, dir string) {
			if runtime.GOOS == "windows" {
				t.Skip("no exec bit")
			}
			if err := os.Chmod(filepath.Join(dir, "a.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"delete the symlink": func(t *testing.T, dir string) {
			isLinkOrSkip(t, filepath.Join(dir, "lnk"))
			if err := os.Remove(filepath.Join(dir, "lnk")); err != nil {
				t.Fatal(err)
			}
		},
		"replace the symlink with a file": func(t *testing.T, dir string) {
			isLinkOrSkip(t, filepath.Join(dir, "lnk"))
			if err := os.Remove(filepath.Join(dir, "lnk")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, "lnk"), "plain\n")
		},
		"delete an empty file": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "e0")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			g := isoGit(t)
			repo := linkRepo(t)
			writeFile(t, filepath.Join(repo, "e0"), "")
			writeFile(t, filepath.Join(repo, "dd.txt"), "-- comment\nkeep\n")
			sh(t, repo, "add", ".")
			sh(t, repo, "commit", "-qm", "more")
			_, co, p, diff := draftT(t, g, repo, func(dir string) { edit(t, dir) })
			if err := Validate(context.Background(), g, co, p, Caps{Deny: noDeny}); err != nil {
				t.Fatalf("Validate: %v\n%s", err, diff)
			}
			if c, err := Apply(context.Background(), g, repo, diff); err != nil || len(c) > 0 {
				t.Fatalf("Apply: %v %v\n%s", err, c, diff)
			}
		})
	}
}

func TestValidateNeedsTheDraftCheckout(t *testing.T) {
	g, _, p := forged(t)
	if err := Validate(context.Background(), g, Checkout{}, p, Caps{Deny: noDeny}); err == nil {
		t.Fatal("Validate passed without the draft to check the patch's paths against")
	}
}

func TestDiffHeadersAcceptsOnlyAStrictIndexLine(t *testing.T) {
	head := "diff --git a/x b/x\n"
	tail := "\n--- a/x\n+++ b/x\n" + bHunk
	for _, ok := range []string{
		"index 2e65efe..f4d5ecc 100644",
		"index " + strings.Repeat("a", 40) + ".." + strings.Repeat("b", 40) + " 100755",
		"index " + strings.Repeat("a", 64) + ".." + strings.Repeat("b", 64) + " 160000",
	} {
		if _, _, err := diffHeaders([]byte(head + ok + tail)); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"index 2e65efe..f4d5ecc\t100644",
		"index 2e65efe..f4d5ecc\v100644",
		"index 2e65efe..f4d5ecc 100644",
		"index 2e65efe 100644",
		"index 2e65efe.f4d5ecc 100644",
		"index " + strings.Repeat("a", 70) + "..f4d5ecc 100644",
		"index 2e65efe..f4d5ecc  100644",
		"index 2e65efe..f4d5ecc 100644 ",
		"index  2e65efe..f4d5ecc 100644",
		"index 2E65EFE..f4d5ecc 100644",
		"index 2e65..f4d5ecc 100644",
		"index 2e65efe..f4d5ecc 100600",
		"index\t2e65efe..f4d5ecc 100644",
	} {
		if _, _, err := diffHeaders([]byte(head + bad + tail)); !errors.Is(err, ErrEscape) {
			t.Errorf("%q: err %v, want ErrEscape", bad, err)
		}
	}
}
