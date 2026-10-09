package toolloop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Critical 1: the deny list must not depend on the filesystem's case rules.
func TestDeniedIsCaseInsensitive(t *testing.T) {
	for _, rel := range []string{".ENV", ".Env.Local", "A/.GIT/config", ".Git", "CONFIG/SECRETS/prod.json", "Credentials.TOML", "home/.ssh/ID_RSA", "X.PEM", "Server.KEY"} {
		if !Denied(rel, DefaultDeny) {
			t.Errorf("%q should be denied", rel)
		}
	}
	if !Denied("Fixtures/PRIVATE/a.json", WithDeny([]string{"**/fixtures/private/**"}).Deny) {
		t.Error("extra globs fold case too")
	}
	if Denied("Main.go", DefaultDeny) {
		t.Error("Main.go should stay allowed")
	}
}

func TestResolveInRootRefusesCaseVariantsOnDisk(t *testing.T) {
	root, _ := repoDir(t)
	if err := os.WriteFile(filepath.Join(root, "Credentials.toml"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ssh", "id_rsa"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".ENV", ".Git/config", ".SSH/ID_RSA", "Credentials.TOML", "CONFIG/SECRETS/prod.json"} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); err == nil {
			t.Errorf("ResolveInRoot(%q) must fail", p)
		} else if !errors.Is(err, ErrDenied) && !strings.Contains(err.Error(), "no such file") {
			t.Errorf("ResolveInRoot(%q) = %v", p, err)
		}
	}
	tools := RepoTools(root, DefaultLimits())
	for _, p := range []string{".ENV", "Credentials.TOML"} {
		if out, err := run(t, tools, "read_file", map[string]any{"path": p}); err == nil || strings.Contains(out, "supersecret") {
			t.Errorf("read_file(%q) = %q %v", p, out, err)
		}
	}
}

// Important 2: git's path list is not trusted.
func TestListIgnoresHostileWorktreeConfig(t *testing.T) {
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "elsewhere.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "mine.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.OpenFile(filepath.Join(root, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = cfg.WriteString("\n[core]\n\tworktree = " + other + "\n")
	cfg.Close()
	out, err := run(t, RepoTools(root, DefaultLimits()), "list_files", map[string]any{})
	if err != nil || strings.Contains(out, "elsewhere") || !strings.Contains(out, "mine.go") {
		t.Fatalf("list:\n%s\n%v", out, err)
	}
}

func TestListIgnoresInheritedGitEnv(t *testing.T) {
	other := t.TempDir()
	gitInit(t, other)
	if err := os.WriteFile(filepath.Join(other, "elsewhere.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", other, "add", "elsewhere.txt").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mine.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	out, err := run(t, RepoTools(root, DefaultLimits()), "list_files", map[string]any{})
	if err != nil || strings.Contains(out, "elsewhere") || !strings.Contains(out, "mine.go") {
		t.Fatalf("list:\n%s\n%v", out, err)
	}
}

func TestGitEnvStripsOverrides(t *testing.T) {
	env := gitEnv([]string{"GIT_DIR=/x", "GIT_WORK_TREE=/y", "GIT_INDEX_FILE=/z", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_GLOBAL=/g",
		"HERDR_SESSION=a", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "PATH=/bin"})
	joined := strings.Join(env, "\n")
	for _, bad := range []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_CONFIG_COUNT", "GIT_CONFIG_GLOBAL", "HERDR_", "CLAUDECODE", "CLAUDE_CODE"} {
		if strings.Contains(joined, bad) {
			t.Errorf("env still has %s", bad)
		}
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "GIT_CONFIG_NOSYSTEM=1") {
		t.Errorf("env %v", env)
	}
}

// Defence in depth (Phase 8 final wave): list_files' git run has fsmonitor
// off, hooks off and every transport refused, and an inherited environment
// can't re-enable lazy fetching or a protocol.
func TestGitFilesHardening(t *testing.T) {
	args := strings.Join(gitFilesArgs("/r/.git", "/r"), "\x00")
	for _, want := range []string{"-c\x00core.fsmonitor=false", "-c\x00core.hooksPath=" + os.DevNull, "-c\x00protocol.allow=never"} {
		if !strings.Contains(args, want) {
			t.Errorf("git args lack %q: %q", want, args)
		}
	}
	env := gitEnv([]string{"PATH=/bin", "GIT_ALLOW_PROTOCOL=file:https", "GIT_NO_LAZY_FETCH=0"})
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, want := range []string{"\nGIT_NO_LAZY_FETCH=1\n", "\nGIT_ALLOW_PROTOCOL=none\n"} {
		if !strings.Contains(joined, want) {
			t.Errorf("env lacks %q: %v", strings.TrimSpace(want), env)
		}
	}
	for _, bad := range []string{"GIT_ALLOW_PROTOCOL=file:https", "GIT_NO_LAZY_FETCH=0"} {
		if strings.Contains(joined, bad) {
			t.Errorf("inherited %s survived: %v", bad, env)
		}
	}
}

// Important 3.
func TestDefaultDenyCoversCommonCredentialFiles(t *testing.T) {
	for _, rel := range []string{"id_dsa", "home/.ssh/config", ".ssh/known_hosts", "u/.gnupg/pubring.kbx", ".aws/config", "u/.config/gh/hosts.yml",
		".kube/config", "u/.docker/config.json", ".git-credentials", "infra/terraform.tfstate", "a/x.tfstate.backup", ".zsh_history", ".bash_history",
		"k.ppk", "sub/.netrc"} {
		if !Denied(rel, DefaultDeny) {
			t.Errorf("%q should be denied", rel)
		}
	}
	for _, rel := range []string{"docs/history.md", "kube/config.go", "src/ssh.go"} {
		if Denied(rel, DefaultDeny) {
			t.Errorf("%q should be allowed", rel)
		}
	}
}

func TestNoToolsForHomeOrAboveIt(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	setHome(t, home)
	for _, r := range []string{home, parent, "/"} {
		if got := RepoTools(r, DefaultLimits()); len(got) != 0 {
			t.Errorf("RepoTools(%q) gave %d tools", r, len(got))
		}
		if !Unavailable(r) {
			t.Errorf("Unavailable(%q) = false", r)
		}
	}
	if got := RepoTools(proj, DefaultLimits()); len(got) != 3 {
		t.Errorf("project below home: %d tools", len(got))
	}
	if !strings.Contains(NoToolsNote, "unavailable") {
		t.Error("NoToolsNote must say tools are unavailable")
	}
}

// Minor 4: no existence oracle outside the root.
func TestNoExistenceOracleOutsideRoot(t *testing.T) {
	root, outside := repoDir(t)
	symlinkOrSkip(t, filepath.Join(outside, "missing.txt"), filepath.Join(root, "dangling.txt"))
	for _, p := range []string{"dangling.txt", "outdir/missing.txt", "outdir/notes.txt"} {
		_, _, err := ResolveInRoot(root, p, DefaultDeny)
		if !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("%q: %v, want ErrOutsideRoot", p, err)
		}
	}
	_, _, err := ResolveInRoot(root, "nope/none.go", DefaultDeny)
	if err == nil || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), outside) {
		t.Errorf("error leaks an absolute path: %v", err)
	}
}

// Minor 5: an over-long line is clipped with a note, not silently dropped.
func TestLongLinesAreClippedAndReadingContinues(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("x", 2<<20) + "\nNEEDLE here\n"
	if err := os.WriteFile(filepath.Join(root, "long.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := RepoTools(root, DefaultLimits())
	out, err := run(t, tools, "read_file", map[string]any{"path": "long.txt"})
	if err != nil || !strings.Contains(out, "line too long, truncated") || !strings.Contains(out, "NEEDLE here") || len(out) > 64<<10 {
		t.Fatalf("read_file: len %d err %v", len(out), err)
	}
	out, err = run(t, tools, "grep", map[string]any{"pattern": "NEEDLE"})
	if err != nil || !strings.Contains(out, "long.txt:2:") {
		t.Fatalf("grep:\n%.200s\n%v", out, err)
	}
}
