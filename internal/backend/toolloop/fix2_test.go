package toolloop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// APFS folds ſ to s and ß to ss; the deny check must not be fooled.
func TestDeniedUsesFullUnicodeCaseFolding(t *testing.T) {
	for _, rel := range []string{"id_rſa", ".htpaßwd", ".ſſh/config", "ſecrets/db.txt", "credentials.jſon", "Secrets/x", ".ENV"} {
		if !Denied(rel, DefaultDeny) {
			t.Errorf("%q should be denied", rel)
		}
	}
	if Denied("Main.go", DefaultDeny) || Denied("straße.go", DefaultDeny) {
		t.Error("ordinary names stay allowed")
	}
}

func TestFoldedNamesAreRefusedThroughTheTools(t *testing.T) {
	root, _ := repoDir(t)
	mustWrite(t, filepath.Join(root, "id_rsa"), "PRIVATE KEY")
	mustWrite(t, filepath.Join(root, ".htpasswd"), "PRIVATE KEY")
	mustWrite(t, filepath.Join(root, ".ssh", "config"), "PRIVATE KEY")
	mustWrite(t, filepath.Join(root, "Readme.md"), "hello")
	tools := RepoTools(root, DefaultLimits())
	for _, p := range []string{"id_rſa", ".htpaßwd", ".ſſh/config", "ID_RSA", ".ENV", "Secrets/../id_rſa", "id_rsa", ".htpasswd", ".ssh/config", ".env"} {
		out, err := run(t, tools, "read_file", map[string]any{"path": p})
		if err == nil || strings.Contains(out, "PRIVATE") || strings.Contains(out, "supersecret") {
			t.Errorf("read_file(%q) = %q %v", p, out, err)
		}
	}
	if out, err := run(t, tools, "read_file", map[string]any{"path": "Readme.md"}); err != nil || !strings.Contains(out, "hello") {
		t.Fatalf("exact name must still work: %q %v", out, err)
	}
}

// Documented behavior: a path must match the on-disk spelling byte for byte,
// so a case variant of an ordinary file is "no such file" even on APFS.
func TestOrdinaryCaseVariantIsNoSuchFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Readme.md"), "hello")
	mustWrite(t, filepath.Join(root, "Dir", "File.go"), "x")
	for _, p := range []string{"README.md", "readme.md", "dir/File.go", "Dir/file.go"} {
		_, _, err := ResolveInRoot(root, p, DefaultDeny)
		if err == nil || !strings.Contains(err.Error(), "no such file") {
			t.Errorf("%q: err = %v, want no such file", p, err)
		}
	}
	if _, rel, err := ResolveInRoot(root, "Dir/File.go", DefaultDeny); err != nil || rel != "Dir/File.go" {
		t.Fatalf("exact: %q %v", rel, err)
	}
}

func TestSymlinkTargetSpellingIsExactToo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "id_rsa"), "PRIVATE KEY")
	symlinkOrSkip(t, "id_rſa", filepath.Join(root, "link.txt"))
	if _, _, err := ResolveInRoot(root, "link.txt", DefaultDeny); err == nil {
		t.Fatal("a link spelled with a folded name must not resolve")
	}
}

func TestListFiltersStaleIndexEntriesUnderSymlinkedParents(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, "real", "a.go"), "x")
	mustWrite(t, filepath.Join(outside, "b.go"), "x")
	if out, err := execGit(root, "add", "real/a.go"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	// Replace the tracked directory with a symlink out of the root: the index
	// still names real/a.go, and real/a.go now "exists" through the link only
	// if the target has it.
	if err := os.RemoveAll(filepath.Join(root, "real")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(outside, "a.go"), "x")
	symlinkOrSkip(t, outside, filepath.Join(root, "real"))
	out, err := run(t, RepoTools(root, DefaultLimits()), "list_files", map[string]any{})
	if err != nil || strings.Contains(out, "real/a.go") {
		t.Fatalf("list leaked via a stale entry:\n%s\n%v", out, err)
	}
}

func TestUnavailableFallsBackWhenHomeIsUnsetAndMatchesCaseVariants(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "Home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	setHome(t, home)
	variant := filepath.Join(parent, "hOME")
	if _, err := os.Stat(variant); err == nil {
		if !Unavailable(variant) {
			t.Error("case variant of home must be unavailable")
		}
	} else {
		t.Log("case-sensitive filesystem: variant check skipped")
	}
	setHome(t, "")
	if h := homeDir(); h == "" {
		t.Error("homeDir must fall back to the user database when HOME is unset")
	}
}

func TestHomeAnchoredGlobsApplyWhenCwdIsBelowHome(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	mustWrite(t, filepath.Join(home, ".config", "gh", "hosts.yml"), "oauth_token: SECRET")
	mustWrite(t, filepath.Join(home, ".config", "ok.txt"), "fine")
	mustWrite(t, filepath.Join(home, ".docker", "config.json"), "SECRET")
	mustWrite(t, filepath.Join(home, ".kube", "config"), "SECRET")
	for _, c := range [][2]string{{".config", "gh/hosts.yml"}, {".docker", "config.json"}, {".kube", "config"}} {
		root := filepath.Join(home, c[0])
		tools := RepoTools(root, DefaultLimits())
		if len(tools) == 0 {
			t.Fatalf("%s: no tools", c[0])
		}
		if out, err := run(t, tools, "read_file", map[string]any{"path": c[1]}); !errors.Is(err, ErrDenied) || strings.Contains(out, "SECRET") {
			t.Errorf("%s/%s: %q %v", c[0], c[1], out, err)
		}
		if out, _ := run(t, tools, "grep", map[string]any{"pattern": "SECRET"}); out != "no matches" {
			t.Errorf("%s grep leaked: %q", c[0], out)
		}
	}
	tools := RepoTools(filepath.Join(home, ".config"), DefaultLimits())
	if out, err := run(t, tools, "read_file", map[string]any{"path": "ok.txt"}); err != nil || !strings.Contains(out, "fine") {
		t.Fatalf("ordinary file under home: %q %v", out, err)
	}
}

func execGit(dir string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
}

func TestListFilesDescriptionExplainsGitignoreScope(t *testing.T) {
	for _, tl := range RepoTools(t.TempDir(), DefaultLimits()) {
		if tl.Name == "list_files" && !strings.Contains(tl.Description, "git repository root") {
			t.Errorf("description: %q", tl.Description)
		}
	}
}
