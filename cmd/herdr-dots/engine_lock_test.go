package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

func lockTestPaths(t *testing.T) paths.Paths {
	t.Helper()
	root, err := os.MkdirTemp("", "hd") // short: the socket stays in the session dir
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	p := paths.New(filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "h.sock"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	return p
}

// E1: lockfile.Held probes with the real lock. An engine starting during a
// client's probe waits it out instead of exiting as if another engine ran.
func TestEngineLockOutwaitsABriefProbe(t *testing.T) {
	p := lockTestPaths(t)
	probe, err := lockfile.TryLock(p.EngineLock())
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(100*time.Millisecond, func() { probe.Release() })
	l, err := lockEngine(p.EngineLock())
	if err != nil {
		t.Fatalf("lockEngine = %v, want the lock once the probe let go", err)
	}
	l.Release()
}

// E5: the loser of two engines for one session returns quietly, without
// serving.
func TestServeEngineLoserReturnsQuietly(t *testing.T) {
	p := lockTestPaths(t)
	held, err := lockfile.TryLock(p.EngineLock())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	done := make(chan error, 1)
	go func() { done <- serveEngine(p) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveEngine = %v, want nil for the loser", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the losing engine did not return")
	}
}

// E3: an engine that never becomes ready points at engine.log.
func TestEnsureEngineNotReadyPointsAtTheLog(t *testing.T) {
	p := lockTestPaths(t)
	held, err := lockfile.TryLock(p.EngineLock()) // "running", but never serving
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	old := engineReadyWait
	engineReadyWait = 100 * time.Millisecond
	t.Cleanup(func() { engineReadyWait = old })
	err = ensureEngine(p)
	if err == nil || !strings.Contains(err.Error(), p.EngineLog()) {
		t.Fatalf("ensureEngine = %v, want an error naming %s", err, p.EngineLog())
	}
}
