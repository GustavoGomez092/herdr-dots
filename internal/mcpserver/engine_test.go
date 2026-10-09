package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

// stubEngine is a minimal engine socket: it answers list_dots for one pane.
type stubEngine struct{ pane string }

func (s stubEngine) Handle(context.Context, string, json.RawMessage) error { return nil }
func (s stubEngine) Snapshot() view.Snapshot                               { return view.Snapshot{} }
func (s stubEngine) Watch() (<-chan view.Snapshot, func()) {
	return make(chan view.Snapshot), func() {}
}
func (s stubEngine) Query(_ context.Context, cmd string, raw json.RawMessage) (any, error) {
	var a ipc.PaneArgs
	_ = json.Unmarshal(raw, &a)
	if cmd != ipc.CmdListDots || a.PaneID != s.pane {
		return nil, errors.New("unexpected " + cmd + " for " + a.PaneID)
	}
	return ipc.DotsResult{ProjectID: testPID, Dots: []ipc.DotInfo{{Slug: "security"}}}, nil
}

func shortSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mcpe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "e.sock")
}

func startStub(t *testing.T, path string) context.CancelFunc {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ipc.Serve(ctx, ln, stubEngine{pane: "w1:p1"})
	return cancel
}

// Review Focus 3: the engine isn't running when the agent first calls a tool.
// Creating the engine client starts nothing; the first call starts the engine
// and succeeds; a dead engine is started again once.
func TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies(t *testing.T) {
	path := shortSock(t)
	ensures := 0
	var stop context.CancelFunc
	e := NewIPCEngine(ipc.Client{Path: path}, "w1:p1", func() error {
		ensures++
		stop = startStub(t, path)
		return nil
	})
	if ensures != 0 {
		t.Fatal("server start must not start the engine")
	}
	res, err := e.ListDots(context.Background())
	if err != nil || ensures != 1 || res.ProjectID != testPID {
		t.Fatalf("first call: %+v %v (ensures %d)", res, err, ensures)
	}
	if _, err := e.ListDots(context.Background()); err != nil || ensures != 1 {
		t.Fatalf("second call: %v (ensures %d)", err, ensures)
	}
	stop() // the engine goes away
	_ = os.Remove(path)
	if _, err := e.ListDots(context.Background()); err != nil || ensures != 2 {
		t.Fatalf("after the engine died: %v (ensures %d)", err, ensures)
	}
	stop()
}

func TestIPCEngineReportsAnEngineThatWontStart(t *testing.T) {
	e := NewIPCEngine(ipc.Client{Path: shortSock(t)}, "w1:p1", func() error { return errors.New("lock held") })
	if _, err := e.ListDots(context.Background()); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("got %v", err)
	}
	e = NewIPCEngine(ipc.Client{Path: shortSock(t)}, "w1:p1", func() error { return nil }) // "starts" but never listens
	if _, err := e.ListDots(context.Background()); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("got %v", err)
	}
}
