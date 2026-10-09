package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/paths"
)

func TestBuildEngineLoadsDotsFromConfigDir(t *testing.T) {
	root := t.TempDir()
	p := paths.New(filepath.Join(root, "c"), filepath.Join(root, "s"), "/fake.sock")
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	d, _ := dots.FromTemplate("security")
	if err := dots.Save(p.DotsDir(), d, true); err != nil {
		t.Fatal(err)
	}
	srv := herdrtest.New(t)
	e, err := buildEngine(p, &herdr.SocketClient{Path: srv.Path}, config.Default(), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if len(s.Library) != 1 || s.Library[0].Slug != "security" {
		t.Fatalf("library %+v", s.Library)
	}
	if err := e.SetMode("security", dots.ModeSuggest); err != nil {
		t.Fatal(err)
	}
	got, _ := dots.Load(p.DotsDir(), "security")
	if got.Delivery.Mode != dots.ModeSuggest {
		t.Fatal("SetMode must persist to the dot file")
	}
}

func TestWaitForSocketTimesOut(t *testing.T) {
	start := time.Now()
	err := waitForSocket(filepath.Join(t.TempDir(), "none.sock"), 200*time.Millisecond)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}

func TestRotateLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.log")
	os.WriteFile(p, make([]byte, maxLogBytes+1), 0o600)
	rotateLog(p)
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal("big log should rotate")
	}
}

func TestOpenEngineLogRotatesBeforeOpening(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.log")
	old := make([]byte, maxLogBytes+1)
	old[0] = 'X'
	os.WriteFile(p, old, 0o600)
	f, err := openEngineLog(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	bak, err := os.ReadFile(p + ".1")
	if err != nil || len(bak) != len(old) || bak[0] != 'X' {
		t.Fatalf("backup should hold old content: err=%v len=%d", err, len(bak))
	}
	f.WriteString("fresh")
	got, _ := os.ReadFile(p)
	if string(got) != "fresh" {
		t.Fatalf("engine.log should be the fresh file, got %d bytes", len(got))
	}
}
