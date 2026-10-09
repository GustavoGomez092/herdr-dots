package toolloop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoDir builds a project with a secret, a binary file, a symlink that
// escapes the root and one that launders a secret's name.
func repoDir(t *testing.T) (root, outside string) {
	t.Helper()
	root, outside = t.TempDir(), t.TempDir()
	write := func(dir, name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(outside, "notes.txt", "PRIVATE KEY MATERIAL")
	write(root, "main.go", "package main\n\nfunc main() {\n\tlogin()\n}\n")
	write(root, "web/app.ts", "export const login = () => fetch('/login')\n")
	write(root, ".env", "API_KEY=supersecret\n")
	write(root, "config/secrets/prod.json", `{"token":"x"}`)
	write(root, "bin/blob.dat", "ab\x00cd")
	if err := os.Symlink(filepath.Join(outside, "notes.txt"), filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(root, "innocent.txt")); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func run(t *testing.T, tools []Tool, name string, args any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	for _, tl := range tools {
		if tl.Name == name {
			return tl.Run(context.Background(), raw)
		}
	}
	t.Fatalf("no tool %s", name)
	return "", nil
}

func TestResolveInRootRefusesEscapes(t *testing.T) {
	root, outside := repoDir(t)
	deny := DefaultDeny
	for _, p := range []string{"../x", "web/../../x", filepath.Join(outside, "notes.txt"), "/etc/passwd", "escape.txt", "outdir/notes.txt"} {
		if _, _, err := ResolveInRoot(root, p, deny); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("ResolveInRoot(%q) err = %v, want ErrOutsideRoot", p, err)
		}
	}
	if _, rel, err := ResolveInRoot(root, filepath.Join(root, "web", "app.ts"), deny); err != nil || rel != "web/app.ts" {
		t.Errorf("absolute path inside the root: %q %v", rel, err)
	}
	if _, rel, err := ResolveInRoot(root, "./web/../main.go", deny); err != nil || rel != "main.go" {
		t.Errorf("clean relative path: %q %v", rel, err)
	}
}

func TestResolveInRootDeniesSecretsEvenThroughSymlinks(t *testing.T) {
	root, _ := repoDir(t)
	for _, p := range []string{".env", "config/secrets/prod.json", "innocent.txt", ".git/config", "deploy/server.pem", "home/id_ed25519.pub"} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); !errors.Is(err, ErrDenied) {
			t.Errorf("ResolveInRoot(%q) err = %v, want ErrDenied", p, err)
		}
	}
}

func TestResolveInRootRefusesNULBytes(t *testing.T) {
	root, _ := repoDir(t)
	for _, p := range []string{"main.go\x00.txt", "\x00", "web/\x00/app.ts", ".env\x00.go"} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); err == nil {
			t.Errorf("ResolveInRoot(%q) must fail", p)
		}
	}
}

func TestDeniedGlobs(t *testing.T) {
	for _, rel := range []string{".env", "a/b/.env.local", "x.key", "sub/.git/HEAD", ".git", "secrets/a", ".netrc", "vault.kdbx"} {
		if !Denied(rel, DefaultDeny) {
			t.Errorf("%q should be denied", rel)
		}
	}
	for _, rel := range []string{"main.go", "envelope.go", "keys.go", "docs/secrets.md", "web/app.ts"} {
		if Denied(rel, DefaultDeny) {
			t.Errorf("%q should be allowed", rel)
		}
	}
	if !Denied("fixtures/private/a.json", WithDeny([]string{"**/fixtures/private/**", "[bad"}).Deny) {
		t.Error("config deny globs add to the list")
	}
}

func TestDenyListCoversCredentialsAndNeverShrinks(t *testing.T) {
	for _, rel := range []string{"credentials.toml", "sub/credentials.toml", ".aws/credentials", ".npmrc", ".pgpass", ".htpasswd", "x/.env.production"} {
		if !Denied(rel, DefaultDeny) {
			t.Errorf("%q should be denied by default", rel)
		}
	}
	l := WithDeny([]string{"", "  ", "extra/**"})
	for _, g := range DefaultDeny {
		found := false
		for _, d := range l.Deny {
			found = found || d == g
		}
		if !found {
			t.Errorf("WithDeny dropped built-in %q", g)
		}
	}
	// The caller's slice must not alias the package default.
	l.Deny[0] = "mutated"
	if DefaultDeny[0] == "mutated" || DefaultLimits().Deny[0] == "mutated" {
		t.Error("Limits.Deny aliases DefaultDeny")
	}
}

func TestReadFileOffsetLimitAndRefusals(t *testing.T) {
	root, _ := repoDir(t)
	tools := RepoTools(root, DefaultLimits())
	out, err := run(t, tools, "read_file", map[string]any{"path": "main.go", "offset": 3, "limit": 2})
	if err != nil || !strings.Contains(out, "     3\tfunc main() {") || !strings.Contains(out, "     4\t\tlogin()") || strings.Contains(out, "package main") {
		t.Fatalf("offset/limit:\n%s\n%v", out, err)
	}
	if !strings.Contains(out, "offset 5") {
		t.Fatalf("missing continuation hint:\n%s", out)
	}
	if _, err := run(t, tools, "read_file", map[string]any{"path": "bin/blob.dat"}); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary: %v", err)
	}
	if _, err := run(t, tools, "read_file", map[string]any{"path": "web"}); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("dir: %v", err)
	}
	if out, err := run(t, tools, "read_file", map[string]any{"path": "escape.txt"}); !errors.Is(err, ErrOutsideRoot) || strings.Contains(out, "PRIVATE") {
		t.Fatalf("escape: %q %v", out, err)
	}
}

// A parent directory swapped for a symlink after the path was checked must not
// let the open leave the root.
func TestOpenRejectsParentSwappedForSymlink(t *testing.T) {
	root, outside := repoDir(t)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "notes.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	abs, _, err := ResolveInRoot(root, "sub/notes.txt", DefaultDeny)
	if err != nil {
		t.Fatal(err)
	}
	// Race: replace sub with a symlink to the outside after the check.
	if err := os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "sub.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	f, err := openVerified(abs)
	if err == nil {
		f.Close()
		t.Fatal("openVerified followed a swapped parent symlink")
	}
}

func TestResultCapAddsMarker(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "big.txt"), []byte(strings.Repeat("0123456789abcdef\n", 400)), 0o600)
	lim := DefaultLimits()
	lim.MaxResultBytes = 1000
	out, err := run(t, RepoTools(root, lim), "read_file", map[string]any{"path": "big.txt"})
	if err != nil || len(out) > 1000 || !strings.HasSuffix(out, truncMarker) {
		t.Fatalf("len %d err %v tail %q", len(out), err, out[max(0, len(out)-60):])
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
}

func TestGrepAndListRespectGitignoreAndDenyList(t *testing.T) {
	root, _ := repoDir(t)
	gitInit(t, root)
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("dist/\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(root, "dist"), 0o700)
	_ = os.WriteFile(filepath.Join(root, "dist", "bundle.js"), []byte("login()\n"), 0o600)
	tools := RepoTools(root, DefaultLimits())

	out, err := run(t, tools, "grep", map[string]any{"pattern": `login`})
	if err != nil || !strings.Contains(out, "main.go:4:") || !strings.Contains(out, "web/app.ts:1:") || strings.Contains(out, "dist/") {
		t.Fatalf("grep:\n%s\n%v", out, err)
	}
	if out, _ := run(t, tools, "grep", map[string]any{"pattern": "supersecret|PRIVATE"}); out != "no matches" {
		t.Fatalf("grep leaked a denied or outside file:\n%s", out)
	}
	if out, _ := run(t, tools, "grep", map[string]any{"pattern": "login", "glob": "**/*.ts"}); strings.Contains(out, "main.go") {
		t.Fatalf("glob filter:\n%s", out)
	}
	list, err := run(t, tools, "list_files", map[string]any{})
	if err != nil || !strings.Contains(list, "web/app.ts") || strings.Contains(list, ".env") || strings.Contains(list, "dist/") || strings.Contains(list, "secrets/") {
		t.Fatalf("list:\n%s\n%v", list, err)
	}
	if _, err := run(t, tools, "grep", map[string]any{"pattern": "("}); err == nil {
		t.Fatal("bad regexp must be a tool error")
	}
}

func TestListWithoutGitWalksAndCaps(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a.go", "b.go", "c.go", ".git/HEAD"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, n)), 0o700)
		_ = os.WriteFile(filepath.Join(root, n), []byte("x"), 0o600)
	}
	lim := DefaultLimits()
	lim.MaxListPaths = 2
	out, err := run(t, RepoTools(root, lim), "list_files", map[string]any{"glob": "**/*"})
	if err != nil || strings.Count(out, ".go") != 2 || !strings.Contains(out, "stopped at 2") || strings.Contains(out, "HEAD") {
		t.Fatalf("list:\n%s\n%v", out, err)
	}
}
