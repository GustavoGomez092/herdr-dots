package main

import (
	"context"
	"path/filepath"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/version"
)

func TestMemoryStoreForFollowsConfig(t *testing.T) {
	p := paths.New("/c", "/s", "/sock")
	cfg := config.Default()
	cfg.MemoryMaxEntries = 100
	st := memoryStoreFor(p, cfg, nil)
	if st == nil || st.Dir != p.MemoryDir() || st.Limits.MaxDot != 100 || st.Limits.MaxProject != 150 || st.Limits.MaxGlobal != 50 {
		t.Fatalf("%+v", st)
	}
	cfg.MemoryEnabled = false
	if memoryStoreFor(p, cfg, nil) != nil {
		t.Fatal("memory_enabled = false means no store")
	}
}

func TestBuildEngineWiresMemory(t *testing.T) {
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
	repo := t.TempDir()
	srv.SetPane(herdr.PaneInfo{PaneID: "w1:p1", Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: repo})
	e, err := buildEngine(p, &herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"}, config.Default(), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(e.Wait)
	t.Cleanup(cancel)
	e.Start(ctx)
	if err := e.Attach(ctx, "w1:p1", []string{"security"}); err != nil {
		t.Fatal(err)
	}
	pv, ok := e.Snapshot().Pane("w1:p1")
	if !ok || pv.ProjectID == "" || pv.ProjectName == "" {
		t.Fatalf("attach must resolve a memory project (path identity for a plain dir): %+v", pv)
	}
}

func TestVersionIsTheReleaseCandidateInTheBinary(t *testing.T) {
	if version.Version != "1.0.0-rc.1" {
		t.Fatalf("version %s, want 1.0.0-rc.1", version.Version)
	}
}
