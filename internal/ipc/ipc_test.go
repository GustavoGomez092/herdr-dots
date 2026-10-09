package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
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
func (f *fakeHandler) Snapshot() view.Snapshot { return view.Snapshot{PausedAll: true} }
func (f *fakeHandler) Watch() (<-chan view.Snapshot, func()) {
	return f.updates, func() { f.mu.Lock(); f.stopped = true; f.mu.Unlock() }
}

func serve(t *testing.T) (*fakeHandler, Client) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "ipc")
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", path)
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
	_ = json.Unmarshal(h.args[0], &a)
	if h.got[0] != CmdAttach || a.PaneID != "w1:p1" || a.Slugs[0] != "security" {
		t.Fatalf("handler got %v %+v", h.got, a)
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
	c := Client{Path: filepath.Join(t.TempDir(), "none.sock")}
	if err := c.Call(context.Background(), CmdPing, nil); err == nil {
		t.Fatal("want dial error")
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
