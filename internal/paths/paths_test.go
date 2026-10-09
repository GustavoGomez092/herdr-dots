package paths

import (
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/platform/platformtest"
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
	if p.DotsDir() != filepath.Join("/c", "dots") || p.ConfigFile() != filepath.Join("/c", "config.toml") {
		t.Fatal("config paths wrong")
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
		platformtest.AssertPrivateDir(t, d)
	}
}

// Review Focus: two Herdr sessions of one user get two engine endpoints.
func TestEngineEndpointIsPerSession(t *testing.T) {
	root := t.TempDir()
	a := New(filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "a", "herdr.sock"))
	b := New(filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "b", "herdr.sock"))
	if a.EngineEndpoint() == b.EngineEndpoint() || a.EngineEndpoint() == "" {
		t.Fatalf("endpoints %q and %q", a.EngineEndpoint(), b.EngineEndpoint())
	}
}
