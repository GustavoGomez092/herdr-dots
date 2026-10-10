package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

// shortWrites shrinks the server's write deadline for one test.
func shortWrites(t *testing.T) {
	t.Helper()
	old := writeTimeout
	writeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { writeTimeout = old })
}

// serveConnOnPipe runs serveConn on one end of an unbuffered pipe (a write
// blocks until the peer reads), writes req from the other end and never
// reads; it reports whether serveConn returned within 2 s.
func serveConnOnPipe(t *testing.T, h Handler, req Request) bool {
	t.Helper()
	srv, cli := net.Pipe()
	t.Cleanup(func() { cli.Close() })
	done := make(chan struct{})
	go func() { serveConn(context.Background(), srv, h); close(done) }()
	go func() { _ = json.NewEncoder(cli).Encode(req) }()
	select {
	case <-done:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// I6: a client that sends a command and never reads the reply can't hold the
// connection goroutine (and so Serve's shutdown wait) forever.
func TestCommandReplyToANonReadingClientTimesOut(t *testing.T) {
	shortWrites(t)
	h := &fakeHandler{updates: make(chan view.Snapshot, 1)}
	if !serveConnOnPipe(t, h, Request{ID: 1, Cmd: CmdPing}) {
		t.Fatal("serveConn blocked on a client that never reads its reply")
	}
}

// I6: the same for a watcher that stops reading snapshots.
func TestWatchToANonReadingClientTimesOut(t *testing.T) {
	shortWrites(t)
	h := &fakeHandler{updates: make(chan view.Snapshot, 1)}
	if !serveConnOnPipe(t, h, Request{ID: 1, Cmd: CmdWatch}) {
		t.Fatal("stream blocked on a watcher that never reads")
	}
}

// I1: cancelling Call's context ends the call at once, with the context's
// error, instead of waiting for the reply or the deadline.
func TestCallHonorsCancellation(t *testing.T) {
	h := newQueryHandler(nil)
	h.slow = 1500 * time.Millisecond // a Call that ignored the cancel would take this long
	c := serveHandler(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	err := c.Call(ctx, "slow", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Call = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Call took %v after cancel", d)
	}
}

// closingHandler's watcher channel is already closed.
type closingHandler struct{ *fakeHandler }

func (c closingHandler) Watch() (<-chan view.Snapshot, func()) {
	ch := make(chan view.Snapshot)
	close(ch)
	return ch, func() {}
}

// I4: a watcher channel the engine closes ends the stream; it doesn't spin
// sending empty snapshots.
func TestWatchEndsWhenTheWatcherChannelCloses(t *testing.T) {
	c := serveHandler(t, closingHandler{&fakeHandler{}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for n := 0; ; n++ {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
			if n > 2 {
				t.Fatal("the server keeps streaming after the watcher channel closed")
			}
		case <-deadline:
			t.Fatal("stream did not end after the watcher channel closed")
		}
	}
}
