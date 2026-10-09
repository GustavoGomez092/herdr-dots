package main

import (
	"path/filepath"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/paths"
)

// Phase 9 wiring: the real engine gets per-dot history, this session's key,
// the trust store in CONFIG_DIR and a git resolver for project dots.
func TestEngineDepsWirePhase9(t *testing.T) {
	root := t.TempDir()
	p := paths.New(filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "herdr.sock"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	srv := herdrtest.New(t)
	d, err := engineDeps(p, &herdr.SocketClient{Path: srv.Path}, config.Default(), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if d.History == nil || d.History.Dir != p.HistoryDir() {
		t.Fatalf("history %+v, want %s", d.History, p.HistoryDir())
	}
	if d.SessionKey == "" || d.SessionKey != paths.SessionKey(p.SocketPath) {
		t.Fatalf("session key %q", d.SessionKey)
	}
	if d.Trust == nil || d.Trust.Path != p.TrustFile() {
		t.Fatalf("trust %+v, want %s", d.Trust, p.TrustFile())
	}
	if d.ResolveRoot == nil {
		t.Fatal("project dots need a repository resolver")
	}
	if h := historyStoreFor(p); h.Dir != p.HistoryDir() {
		t.Fatalf("historyStoreFor %s", h.Dir)
	}
}
