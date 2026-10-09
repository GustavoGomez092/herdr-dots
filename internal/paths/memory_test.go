package paths

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

func TestMemoryDirIsSharedAcrossSessions(t *testing.T) {
	c, s := platformtest.Abs("c"), platformtest.Abs("s")
	p := New(c, s, "/sock")
	if p.MemoryDir() != filepath.Join(s, "memory") {
		t.Fatalf("memory dir %q", p.MemoryDir())
	}
	if strings.HasPrefix(p.MemoryDir(), p.SessionDir) {
		t.Fatal("memory must not live under the per-session dir")
	}
	if New(c, s, "/other.sock").MemoryDir() != p.MemoryDir() {
		t.Fatal("two Herdr sessions must share one memory dir")
	}
}

func TestEnsureCreatesMemoryDirAndStatePointer(t *testing.T) {
	root := t.TempDir()
	p := New(filepath.Join(root, "c"), filepath.Join(root, "s"), "/sock")
	for i := 0; i < 2; i++ { // idempotent
		if err := p.Ensure(); err != nil {
			t.Fatal(err)
		}
	}
	platformtest.AssertPrivateDir(t, p.MemoryDir())
	b, err := os.ReadFile(p.StatePointerFile())
	if err != nil || strings.TrimSpace(string(b)) != p.StateDir {
		t.Fatalf("pointer = %q, %v", b, err)
	}
	platformtest.AssertPrivateFile(t, p.StatePointerFile())
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDiscoverPrefersHerdrEnv(t *testing.T) {
	run := func(context.Context, string, ...string) (string, error) {
		t.Fatal("must not shell out when the env is set")
		return "", nil
	}
	c, s := platformtest.Abs("c"), platformtest.Abs("s")
	p, err := discover(env(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": c, "HERDR_PLUGIN_STATE_DIR": s}), run)
	if err != nil || p.ConfigDir != c || p.StateDir != s || p.MemoryDir() != filepath.Join(s, "memory") {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestDiscoverAsksHerdrAndReadsStatePointer(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	writer := New(cfg, filepath.Join(root, "state"), "/sock")
	if err := writer.Ensure(); err != nil {
		t.Fatal(err)
	}
	var got []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		got = append([]string{name}, args...)
		return cfg + "\n", nil
	}
	p, err := discover(env(nil), run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "herdr plugin config-dir herdr-dots" {
		t.Fatalf("ran %q", got)
	}
	if p.ConfigDir != cfg || p.StateDir != writer.StateDir {
		t.Fatalf("%+v", p)
	}
}

func TestDiscoverWithoutPointerExplains(t *testing.T) {
	cfg := t.TempDir()
	run := func(context.Context, string, ...string) (string, error) { return cfg, nil }
	_, err := discover(env(nil), run)
	if err == nil || !strings.Contains(err.Error(), "HERDR_PLUGIN_STATE_DIR") {
		t.Fatalf("want a hint about HERDR_PLUGIN_STATE_DIR, got %v", err)
	}
}

func TestDiscoverWithoutHerdrExplains(t *testing.T) {
	run := func(context.Context, string, ...string) (string, error) {
		return "", errors.New("executable file not found")
	}
	_, err := discover(env(nil), run)
	if err == nil || !strings.Contains(err.Error(), "config-dir") {
		t.Fatalf("want a config-dir hint, got %v", err)
	}
}

func TestDiscoverRejectsRelativePointer(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	os.MkdirAll(cfg, 0o700)
	// Write a relative path to the state pointer
	os.WriteFile(filepath.Join(cfg, ".state-dir"), []byte("relative/path\n"), 0o600)
	run := func(context.Context, string, ...string) (string, error) {
		return cfg + "\n", nil
	}
	_, err := discover(env(nil), run)
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("want error about not absolute, got %v", err)
	}
}

func TestDiscoverRejectsRelativeConfigDir(t *testing.T) {
	run := func(context.Context, string, ...string) (string, error) {
		return "relative/config\n", nil
	}
	_, err := discover(env(nil), run)
	if err == nil || !strings.Contains(err.Error(), "relative") {
		t.Fatalf("want error about relative path, got %v", err)
	}
}

func TestDiscoverRejectsRelativeStateDir(t *testing.T) {
	run := func(context.Context, string, ...string) (string, error) {
		return "", nil
	}
	_, err := discover(env(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": platformtest.Abs("c"), "HERDR_PLUGIN_STATE_DIR": "relative/state"}), run)
	if err == nil || !strings.Contains(err.Error(), "not absolute") || !strings.Contains(err.Error(), "HERDR_PLUGIN_STATE_DIR") {
		t.Fatalf("want error about HERDR_PLUGIN_STATE_DIR not absolute, got %v", err)
	}
}

func TestDiscoverCleansAbsolutePathWithDotDot(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	state := filepath.Join(root, "state")
	os.MkdirAll(cfg, 0o700)
	os.MkdirAll(state, 0o700)
	// Test that paths with .. in them are cleaned
	p, err := discover(env(map[string]string{
		"HERDR_PLUGIN_CONFIG_DIR": filepath.Join(cfg, "a", "..", "b", "..", "."),
		"HERDR_PLUGIN_STATE_DIR":  filepath.Join(state, "sub", ".."),
	}), nil)
	if err != nil {
		t.Fatalf("discover with .. in absolute paths: %v", err)
	}
	if p.ConfigDir != cfg || p.StateDir != state {
		t.Fatalf("paths not cleaned: cfg=%s (want %s), state=%s (want %s)", p.ConfigDir, cfg, p.StateDir, state)
	}
}
