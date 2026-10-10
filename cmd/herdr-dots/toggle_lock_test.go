package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/paths"
)

// gateHerdr opens a Dots pane per target. Each OpenPluginPane waits (up to
// wait) until `want` opens are in flight at once, so two unserialized
// toggles both read the map before either writes it.
type gateHerdr struct {
	herdr.Client
	want int
	wait time.Duration

	mu      sync.Mutex
	arrived int
	all     chan struct{}
}

func (g *gateHerdr) PaneGet(context.Context, string) (herdr.PaneInfo, error) {
	return herdr.PaneInfo{}, errors.New("pane_not_found")
}

func (g *gateHerdr) OpenPluginPane(_ context.Context, req herdr.OpenPane) (string, error) {
	g.mu.Lock()
	g.arrived++
	if g.arrived == g.want {
		close(g.all)
	}
	g.mu.Unlock()
	select {
	case <-g.all:
	case <-time.After(g.wait):
	}
	return "dots-" + req.TargetPaneID, nil
}

// E2: two toggles at the same instant (two panes) both land in
// dotspanes.json; neither overwrites the other's entry.
func TestConcurrentTogglesKeepBothPanes(t *testing.T) {
	dir := t.TempDir()
	p := paths.New(filepath.Join(dir, "c"), filepath.Join(dir, "s"), "/sock")
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	hc := &gateHerdr{want: 2, wait: time.Second, all: make(chan struct{})}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, target := range []string{"w1:p1", "w1:p2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = togglePane(context.Background(), p, hc, target)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	m, err := loadDotsPanes(p.DotsPanesFile())
	if err != nil {
		t.Fatal(err)
	}
	if m["w1:p1"] != "dots-w1:p1" || m["w1:p2"] != "dots-w1:p2" {
		t.Fatalf("dotspanes = %v, want both panes", m)
	}
}
