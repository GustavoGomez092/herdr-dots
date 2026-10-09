package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/keys"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

const herdrConfig = "# mine\n[keys]\nprefix = \"ctrl+a\"\n\n[[keys.command]]\nkey = \"prefix+up\"\ntype = \"plugin_action\"\ncommand = \"cloudmanic.herdr-plus.projects\"\n"

type uninstallRig struct {
	u       uninstallIO
	out     *bytes.Buffer
	p       paths.Paths
	mcpRuns []bool
}

func newUninstallRig(t *testing.T, input string) *uninstallRig {
	t.Helper()
	base := t.TempDir()
	r := &uninstallRig{out: &bytes.Buffer{}}
	r.p = paths.New(filepath.Join(base, "plugins", "config", "herdr-dots"), filepath.Join(base, "plugins", "state", "herdr-dots"), "/tmp/h.sock")
	for _, d := range []string{r.p.DotsDir(), filepath.Join(r.p.StateDir, "memory", "g-0123456789abcdef")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	kp := filepath.Join(base, "herdr", "config.toml")
	if err := os.MkdirAll(filepath.Dir(kp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kp, []byte(herdrConfig+keys.Block(keys.Defaults())), 0o644); err != nil {
		t.Fatal(err)
	}
	r.u = uninstallIO{in: strings.NewReader(input), out: r.out, keysPath: kp,
		removeMCP: func(yes bool) error { r.mcpRuns = append(r.mcpRuns, yes); return nil },
		paths:     func() (paths.Paths, error) { return r.p, nil }, home: filepath.Join(base, "home"),
		now: func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }}
	return r
}

func TestUninstallRemovesMCPAndKeysWithABackupAndKeepsData(t *testing.T) {
	r := newUninstallRig(t, "")
	if err := uninstallExtras([]string{"--yes"}, r.u); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	if len(r.mcpRuns) != 1 || !r.mcpRuns[0] {
		t.Fatalf("setup-mcp --remove --yes runs once: %v", r.mcpRuns)
	}
	got, _ := os.ReadFile(r.u.keysPath)
	if string(got) != herdrConfig {
		t.Fatalf("config after uninstall:\n%s", got)
	}
	if st, err := os.Stat(r.u.keysPath); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o644) {
		t.Fatalf("the config keeps its mode: %v", err)
	}
	backup, err := os.ReadFile(r.u.keysPath + ".bak-herdr-dots-20261008-120000")
	if err != nil || !strings.Contains(string(backup), "herdr-dots.manage") {
		t.Fatalf("backup: %v", err)
	}
	if _, err := os.Stat(r.p.DotsDir()); err != nil {
		t.Fatal("without --purge the dots stay")
	}
	if !strings.Contains(r.out.String(), "--purge") {
		t.Fatalf("output should mention --purge:\n%s", r.out.String())
	}
}

func TestUninstallAsksBeforeEditingTheConfig(t *testing.T) {
	r := newUninstallRig(t, "n\n")
	if err := uninstallExtras(nil, r.u); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(r.u.keysPath); !strings.Contains(string(got), "herdr-dots.manage") {
		t.Fatal("answering n must keep the keybindings")
	}
	if len(r.mcpRuns) != 1 || r.mcpRuns[0] {
		t.Fatalf("setup-mcp asks for itself without --yes: %v", r.mcpRuns)
	}
}

func TestUninstallContinuesWhenMCPRemovalFails(t *testing.T) {
	r := newUninstallRig(t, "")
	r.u.removeMCP = func(bool) error { return errors.New("claude CLI not found") }
	if err := uninstallExtras([]string{"--yes"}, r.u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "claude CLI not found (continuing)") {
		t.Fatalf("output:\n%s", r.out.String())
	}
	if got, _ := os.ReadFile(r.u.keysPath); string(got) != herdrConfig {
		t.Fatal("keys must still be removed")
	}
}

func TestPurgeNeedsTheTypedPhraseEvenWithYes(t *testing.T) {
	r := newUninstallRig(t, "yes\n")
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.p.StateDir); err != nil {
		t.Fatal("a wrong confirmation must delete nothing")
	}
	r2 := newUninstallRig(t, purgeConfirm+"\n")
	if err := uninstallExtras([]string{"--yes", "--purge"}, r2.u); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{r2.p.ConfigDir, r2.p.StateDir} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s survived the purge", d)
		}
	}
}

func TestPurgeRefusesWhileAnEngineRuns(t *testing.T) {
	r := newUninstallRig(t, purgeConfirm+"\n")
	if err := os.MkdirAll(r.p.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.TryLock(r.p.EngineLock())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	err = uninstallExtras([]string{"--yes", "--purge"}, r.u)
	if err == nil || !strings.Contains(err.Error(), "quit Herdr first") {
		t.Fatalf("purge with a running engine: %v", err)
	}
	if _, err := os.Stat(r.p.StateDir); err != nil {
		t.Fatal("nothing may be deleted")
	}
}

func TestSafeToPurgeRefusesHomeRootAndRelativePaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home", "me") // absolute on every OS
	root := filepath.VolumeName(home) + string(filepath.Separator)
	for _, d := range []string{"", filepath.Join("relative", "dir"), root, filepath.Dir(home), home, filepath.Join(root, "a", "b")} {
		if safeToPurge(d, home) {
			t.Errorf("safeToPurge(%q) = true", d)
		}
	}
	if !safeToPurge(filepath.Join(home, ".config", "herdr", "plugins", "herdr-dots"), home) {
		t.Error("a plugin dir under home must be purgeable")
	}
}

func TestSafeToPurgeRefusesOrdinaryDirectoriesUnderHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home", "me")
	for _, d := range []string{".config", "Documents", "Desktop", filepath.Join(".config", "herdr"), filepath.Join(".config", "herdr", "plugins"), filepath.Join("Documents", "taxes")} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
		if safeToPurge(filepath.Join(home, d), home) {
			t.Errorf("safeToPurge(~/%s) = true", d)
		}
	}
	// Generic markers alone are not enough, anywhere.
	for name, mk := range map[string]string{
		filepath.Join("Library", "Caches"): filepath.Join("0123456789ab", "x"),
		filepath.Join("code", "notes"):     filepath.Join("memory", "g"),
		filepath.Join("code", "other"):     filepath.Join("dots", "x.toml"),
		filepath.Join("code", "cfg"):       "credentials.toml",
	} {
		d := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(d, mk)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, mk), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if safeToPurge(d, home) {
			t.Errorf("~/%s with marker %s must be refused", name, mk)
		}
	}
}

func TestSafeToPurgeRefusesAliasesOfFirstLevelHomeDirs(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "me")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(home, ".config"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if safeToPurge(link, home) {
		t.Error("a symlink to ~/.config must be refused")
	}
	homeLink := filepath.Join(base, "homelink")
	if err := os.Symlink(home, homeLink); err != nil {
		t.Skip(err)
	}
	if safeToPurge(homeLink, home) {
		t.Error("a link to home must be refused")
	}
	// On a case-insensitive filesystem ~/.CONFIG is ~/.config.
	if _, err := os.Stat(filepath.Join(home, ".CONFIG")); err == nil && safeToPurge(filepath.Join(home, ".CONFIG"), home) {
		t.Error("a case variant of ~/.config must be refused")
	}
}

func TestPurgeAcceptsARealPluginLayoutAndRefusesAMismatchedOne(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "me")
	cfg := filepath.Join(home, ".config", "herdr", "plugins", "cfgdir")
	st := filepath.Join(home, ".local", "state", "stdir")
	other := filepath.Join(home, ".local", "state", "elsewhere")
	for _, d := range []string{cfg, st, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	u := uninstallIO{home: home, keysPath: filepath.Join(home, ".config", "herdr", "config.toml")}
	p := paths.New(cfg, st, "/tmp/h.sock")
	if err := purgePreflight(u, p); err == nil {
		t.Fatal("names that are not herdr-dots and no pointer: refuse")
	}
	if err := os.WriteFile(filepath.Join(cfg, ".state-dir"), []byte(st+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := purgePreflight(u, p); err != nil {
		t.Fatalf("a layout whose .state-dir names the state dir: %v", err)
	}
	if err := purgePreflight(u, paths.New(cfg, other, "/tmp/h.sock")); err == nil {
		t.Fatal("a state dir that the pointer does not name must be refused")
	}
}

func TestUninstallPreviewNeverListsAUserLineWhenTheConfigHadNoTrailingNewline(t *testing.T) {
	r := newUninstallRig(t, "n\n")
	user := strings.TrimSuffix(herdrConfig, "\n") // setup-keys's leading newline ended this last line
	if err := os.WriteFile(r.u.keysPath, []byte(user+keys.Block(keys.Defaults())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := uninstallExtras(nil, r.u); err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimPrefix(keys.Block(keys.Defaults()), "\n"), "\n")
	for _, l := range strings.Split(r.out.String(), "\n") {
		if !strings.HasPrefix(l, "- ") && l != "-" {
			continue
		}
		body := strings.TrimPrefix(strings.TrimPrefix(l, "-"), " ")
		found := false
		for _, w := range want {
			found = found || w == body
		}
		if !found {
			t.Errorf("preview lists a line that is not ours: %q", l)
		}
	}
}

func TestPurgeRefusesDirsThatContainEachOtherOrTheHerdrConfig(t *testing.T) {
	r := newUninstallRig(t, purgeConfirm+"\n")
	// The state dir lives inside the config dir.
	r.p.StateDir = filepath.Join(r.p.ConfigDir, "herdr-dots")
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err == nil || !strings.Contains(err.Error(), "contains") {
		t.Fatalf("nested dirs: %v", err)
	}
	r2 := newUninstallRig(t, purgeConfirm+"\n")
	r2.u.keysPath = filepath.Join(r2.p.ConfigDir, "config.toml") // Herdr's config inside the plugin dir
	if err := os.WriteFile(r2.u.keysPath, []byte(keys.Block(keys.Defaults())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uninstallExtras([]string{"--yes", "--purge"}, r2.u); err == nil || !strings.Contains(err.Error(), "contains") {
		t.Fatalf("keys path inside: %v", err)
	}
	if len(r.mcpRuns)+len(r2.mcpRuns) != 0 {
		t.Fatal("a refused purge must change nothing")
	}
}

// lockOnRead takes the engine lock the first time the confirmation is read:
// an engine that starts between the check and the typed phrase.
type lockOnRead struct {
	r    io.Reader
	path string
	l    *lockfile.Lock
	t    *testing.T
}

func (x *lockOnRead) Read(b []byte) (int, error) {
	if x.l == nil {
		l, err := lockfile.TryLock(x.path)
		if err != nil {
			x.t.Fatal(err)
		}
		x.l = l
	}
	return x.r.Read(b)
}

func TestPurgeRechecksEnginesAfterTheConfirmation(t *testing.T) {
	r := newUninstallRig(t, "")
	if err := os.MkdirAll(r.p.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	x := &lockOnRead{r: strings.NewReader(purgeConfirm + "\n"), path: r.p.EngineLock(), t: t}
	r.u.in = x
	err := uninstallExtras([]string{"--yes", "--purge"}, r.u)
	if x.l != nil {
		defer x.l.Release()
	}
	if x.l == nil || err == nil || !strings.Contains(err.Error(), "quit Herdr first") {
		t.Fatalf("engine started after the check: %v", err)
	}
	for _, d := range []string{r.p.ConfigDir, r.p.StateDir, r.p.DotsDir()} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("nothing may be deleted: %v", err)
		}
	}
}

func TestPurgeRefusesUnreadableStateDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory modes")
	}
	r := newUninstallRig(t, purgeConfirm+"\n")
	if err := os.Chmod(r.p.StateDir, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(r.p.StateDir, 0o700)
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err == nil {
		t.Fatal("an unreadable state dir may hide a running engine: refuse")
	}
}

func TestPurgeRefusesSymlinkedDirsAndKeepsTheTarget(t *testing.T) {
	for _, which := range []string{"config", "state"} {
		r := newUninstallRig(t, purgeConfirm+"\n")
		dir := &r.p.ConfigDir
		if which == "state" {
			dir = &r.p.StateDir
		}
		target := filepath.Join(t.TempDir(), "real", "herdr-dots")
		if err := os.MkdirAll(filepath.Join(target, "memory"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(*dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, *dir); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := uninstallExtras([]string{"--yes", "--purge"}, r.u)
		real, _ := filepath.EvalSymlinks(target)
		if err == nil || !strings.Contains(err.Error(), "is a link to") || !strings.Contains(err.Error(), real) || !strings.Contains(err.Error(), "remove that yourself") {
			t.Fatalf("%s: %v", which, err)
		}
		if strings.Contains(r.out.String(), "Deleted") {
			t.Fatalf("%s: must not claim a deletion:\n%s", which, r.out.String())
		}
		if _, err := os.Stat(filepath.Join(target, "memory")); err != nil {
			t.Fatalf("%s: the target must survive", which)
		}
		if len(r.mcpRuns) != 0 {
			t.Fatalf("%s: a refused purge must change nothing", which)
		}
	}
}

func TestRefusedPurgeLeavesMCPAndKeysUntouched(t *testing.T) {
	r := newUninstallRig(t, purgeConfirm+"\n")
	if err := os.MkdirAll(r.p.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.TryLock(r.p.EngineLock())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err == nil {
		t.Fatal("want a refusal")
	}
	if len(r.mcpRuns) != 0 {
		t.Fatalf("MCP touched: %v", r.mcpRuns)
	}
	if got, _ := os.ReadFile(r.u.keysPath); !strings.Contains(string(got), "herdr-dots.manage") {
		t.Fatal("keys touched")
	}
}

func TestPurgeRefusesASymlinkedEngineLockAndLeavesItsTarget(t *testing.T) {
	r := newUninstallRig(t, purgeConfirm+"\n")
	if err := os.MkdirAll(r.p.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, r.p.EngineLock()); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err == nil {
		t.Fatal("a symlinked engine.lock cannot be checked: refuse")
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep me\n" {
		t.Fatalf("target overwritten: %q", b)
	}
	if _, err := os.Stat(r.p.StateDir); err != nil {
		t.Fatal("nothing may be deleted")
	}
}

func TestPurgeWithoutTheRightPhraseSaysNothingWasDeleted(t *testing.T) {
	r := newUninstallRig(t, "no\n")
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "Not confirmed. Nothing deleted.") {
		t.Fatalf("output:\n%s", r.out.String())
	}
}

func TestUninstallPreviewsTheExactLinesAndAbbreviatesHome(t *testing.T) {
	r := newUninstallRig(t, "n\n")
	r.u.home = filepath.Dir(filepath.Dir(r.u.keysPath)) // the rig's base dir
	if err := uninstallExtras(nil, r.u); err != nil {
		t.Fatal(err)
	}
	out := r.out.String()
	for _, want := range []string{"- command = \"herdr-dots.manage\"", "- [[keys.command]]", "~" + string(filepath.Separator) + filepath.Join("herdr", "config.toml")} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, r.u.home) {
		t.Fatalf("home not abbreviated:\n%s", out)
	}
}
