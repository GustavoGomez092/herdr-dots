package main

import (
	"bytes"
	"errors"
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
