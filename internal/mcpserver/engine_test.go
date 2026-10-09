package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/platform/platformtest"
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

// shortSock is a fresh engine endpoint: a short unix socket path, or a named
// pipe on Windows.
func shortSock(t *testing.T) string {
	t.Helper()
	return platformtest.Endpoint(t)
}

func startStub(t *testing.T, path string) context.CancelFunc {
	t.Helper()
	ln, err := ipc.Listen(path)
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

// A caller's cancel is not "the engine is down".
func TestIPCEngineCancelIsNotEngineDown(t *testing.T) {
	e := NewIPCEngine(ipc.Client{Path: shortSock(t)}, "w1:p1", func() error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.ListDots(ctx); errors.Is(err, ErrEngineDown) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

// A client cancel stops the wait for a slow engine start.
func TestIPCEngineStartWaitFollowsContext(t *testing.T) {
	release := make(chan struct{})
	e := NewIPCEngine(ipc.Client{Path: shortSock(t)}, "w1:p1", func() error { <-release; return nil })
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.ListDots(ctx); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the wait for the engine start ignored the context")
	}
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
