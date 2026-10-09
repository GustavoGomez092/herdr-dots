package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionKeyStableAndShort(t *testing.T) {
	a := SessionKey("/x/herdr.sock")
	if a != SessionKey("/x/herdr.sock") || len(a) != 12 {
		t.Fatalf("key %q", a)
	}
	if a == SessionKey("/y/herdr.sock") {
		t.Fatal("different sockets must give different keys")
	}
}

func TestFilesLiveUnderSessionDir(t *testing.T) {
	p := New("/c", "/s", "/sock")
	if p.SessionDir != filepath.Join("/s", SessionKey("/sock")) {
		t.Fatalf("session dir %q", p.SessionDir)
	}
	for _, f := range []string{p.EngineLock(), p.QueueFile(), p.AttachmentsFile(), p.FeedFile(), p.UsageFile(), p.EngineLog(), p.DotsPanesFile()} {
		if !strings.HasPrefix(f, p.SessionDir) {
			t.Fatalf("%s not under session dir", f)
		}
	}
	if p.DotsDir() != "/c/dots" || p.ConfigFile() != "/c/config.toml" {
		t.Fatal("config paths wrong")
	}
}

// Review Focus 1: macOS caps unix socket paths at 104 bytes.
func TestEngineSockFallsBackWhenPathTooLong(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	p := New("/c", long, "/sock")
	s := p.EngineSock()
	if len(s) > 100 {
		t.Fatalf("socket path too long (%d): %s", len(s), s)
	}
	if !strings.HasPrefix(s, os.TempDir()) {
		t.Fatalf("expected temp-dir fallback, got %s", s)
	}
	// Verify fallback uses per-uid directory
	expectedFallbackDir := p.sockFallbackDir()
	if !strings.HasPrefix(s, expectedFallbackDir) {
		t.Fatalf("expected fallback in per-uid dir %s, got %s", expectedFallbackDir, s)
	}
	short := New("/c", "/s", "/sock").EngineSock()
	if short != filepath.Join(New("/c", "/s", "/sock").SessionDir, "dots.sock") {
		t.Fatalf("short path should stay in session dir: %s", short)
	}
}

func TestFromEnvRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("want error outside herdr")
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "/c")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/s")
	t.Setenv("HERDR_SOCKET_PATH", "/sock")
	p, err := FromEnv()
	if err != nil || p.ConfigDir != "/c" {
		t.Fatalf("FromEnv: %v %+v", err, p)
	}
}

func TestEnsureCreatesPrivateDirs(t *testing.T) {
	root := t.TempDir()
	p := New(filepath.Join(root, "c"), filepath.Join(root, "s"), "/sock")
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir} {
		st, err := os.Stat(d)
		if err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", d, err, st)
		}
	}
}

func TestEnsureCreatesFallbackDirWhenNeeded(t *testing.T) {
	// Use a long state directory path that triggers the fallback
	root := t.TempDir()
	long := filepath.Join(root, strings.Repeat("d", 100))
	p := New(filepath.Join(root, "c"), long, "/sock")

	// Ensure the fallback directory doesn't exist yet
	fallbackDir := p.sockFallbackDir()
	t.Cleanup(func() {
		// Clean up the fallback directory after the test
		os.RemoveAll(fallbackDir)
	})

	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}

	// Verify fallback directory was created with 0700 permissions
	st, err := os.Stat(fallbackDir)
	if err != nil {
		t.Fatalf("fallback dir not created: %v", err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("fallback dir perm = %v, want 0700", st.Mode().Perm())
	}

	// Verify the engine socket path is still within the limit
	sock := p.EngineSock()
	if len(sock) > 100 {
		t.Fatalf("fallback socket path too long (%d): %s", len(sock), sock)
	}
}
