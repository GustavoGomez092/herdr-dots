package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

type fakeHandler struct {
	mu      sync.Mutex
	got     []string
	args    []json.RawMessage
	updates chan view.Snapshot
	stopped bool
}

func (f *fakeHandler) Handle(_ context.Context, cmd string, args json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, cmd)
	f.args = append(f.args, args)
	if cmd == "explode" {
		return errors.New("boom")
	}
	return nil
}

// calls copies the commands and args the handler goroutines recorded (under
// f.mu, as they wrote them).
func (f *fakeHandler) calls() ([]string, []json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...), append([]json.RawMessage(nil), f.args...)
}

func (f *fakeHandler) Snapshot() view.Snapshot { return view.Snapshot{PausedAll: true} }
func (f *fakeHandler) Watch() (<-chan view.Snapshot, func()) {
	return f.updates, func() { f.mu.Lock(); f.stopped = true; f.mu.Unlock() }
}

func serve(t *testing.T) (*fakeHandler, Client) {
	t.Helper()
	path := testEndpoint(t)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHandler{updates: make(chan view.Snapshot, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go Serve(ctx, ln, h)
	return h, Client{Path: path}
}

func TestCallRoundTripAndErrors(t *testing.T) {
	h, c := serve(t)
	ctx := context.Background()
	if err := c.Call(ctx, CmdAttach, AttachArgs{PaneID: "w1:p1", Slugs: []string{"security"}}); err != nil {
		t.Fatal(err)
	}
	var a AttachArgs
	got, args := h.calls()
	if err := json.Unmarshal(args[0], &a); err != nil {
		t.Fatal(err)
	}
	if got[0] != CmdAttach || a.PaneID != "w1:p1" || len(a.Slugs) != 1 || a.Slugs[0] != "security" {
		t.Fatalf("handler got %v %+v", got, a)
	}
	if err := c.Call(ctx, "explode", nil); err == nil || err.Error() != "boom" {
		t.Fatalf("want boom, got %v", err)
	}
	if err := c.Call(ctx, CmdPing, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWatchStreamsInitialAndUpdates(t *testing.T) {
	h, c := serve(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := <-ch
	if !first.PausedAll {
		t.Fatal("first message is the current snapshot")
	}
	h.updates <- view.Snapshot{InjectEnabled: true}
	select {
	case s := <-ch:
		if !s.InjectEnabled {
			t.Fatal("update not delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("no update")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		stopped := h.stopped
		h.mu.Unlock()
		if stopped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not stop watching after client left")
}

func TestCallFailsFastWithoutServer(t *testing.T) {
	c := Client{Path: testEndpoint(t)} // a real, unserved endpoint on this OS (a pipe name on Windows)
	start := time.Now()
	if err := c.Call(context.Background(), CmdPing, nil); err == nil {
		t.Fatal("want dial error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v; a missing server must fail fast", d)
	}
}

// Review Focus: two Herdr sessions of one user run two engines on two
// endpoints; a client of one never reaches the other.
func TestTwoEndpointsNeverCrossTalk(t *testing.T) {
	ha, ca := serve(t)
	hb, cb := serve(t)
	if ca.Path == cb.Path {
		t.Fatal("test endpoints must differ")
	}
	ctx := context.Background()
	if err := ca.Call(ctx, CmdPauseAll, nil); err != nil {
		t.Fatal(err)
	}
	if err := cb.Call(ctx, CmdReloadDots, nil); err != nil {
		t.Fatal(err)
	}
	ha.mu.Lock()
	hb.mu.Lock()
	defer ha.mu.Unlock()
	defer hb.mu.Unlock()
	if len(ha.got) != 1 || ha.got[0] != CmdPauseAll || len(hb.got) != 1 || hb.got[0] != CmdReloadDots {
		t.Fatalf("engine A got %v, engine B got %v", ha.got, hb.got)
	}
}

func TestCouncilArgsRoundTrip(t *testing.T) {
	h, c := serve(t)
	want := CouncilArgs{PaneID: "w1:p1", Question: "Ready to merge?", Slugs: []string{"security", "copy"}}
	if err := c.Call(context.Background(), CmdCouncil, want); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), CmdCouncilPreflight, CouncilArgs{PaneID: "w1:p1"}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.got) != 2 || h.got[0] != "council" || h.got[1] != "council_preflight" {
		t.Fatalf("commands %v", h.got)
	}
	var got CouncilArgs
	if err := json.Unmarshal(h.args[0], &got); err != nil || got.PaneID != want.PaneID || got.Question != want.Question || len(got.Slugs) != 2 {
		t.Fatalf("args %s: %+v %v", h.args[0], got, err)
	}
	if string(h.args[1]) != `{"pane_id":"w1:p1"}` {
		t.Fatalf("empty question and slugs are omitted: %s", h.args[1])
	}
}
