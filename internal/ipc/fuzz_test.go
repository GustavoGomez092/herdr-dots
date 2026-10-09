package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

// fuzzHandler decodes every argument shape the engine uses and fails
// unknown commands, like the engine does.
type fuzzHandler struct{}

func (fuzzHandler) Handle(_ context.Context, cmd string, raw json.RawMessage) error {
	var v struct {
		PaneID string   `json:"pane_id"`
		Slugs  []string `json:"slugs"`
		IDs    []string `json:"ids"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
	}
	if cmd == CmdPing {
		return nil
	}
	return errors.New("unknown command")
}
func (fuzzHandler) Snapshot() view.Snapshot { return view.Snapshot{} }
func (fuzzHandler) Watch() (<-chan view.Snapshot, func()) {
	return make(chan view.Snapshot), func() {}
}

// FuzzIPCDecode feeds arbitrary bytes to one server connection. The server
// must answer or hang up, never panic or block once the client is done.
func FuzzIPCDecode(f *testing.F) {
	f.Add(`{"id":1,"cmd":"ping"}` + "\n")
	f.Add(`{"id":2,"cmd":"attach","args":{"pane_id":"w1:p1","slugs":["security"]}}` + "\n" + `{"id":3,"cmd":"bogus"}` + "\n")
	f.Add(`{"id":4,"cmd":"watch"}`)
	f.Add(`{"id":"x","cmd":7}`)
	f.Add(`{"id":5,"cmd":"send","args":"not an object"}`)
	f.Fuzz(func(t *testing.T, data string) {
		srv, cli := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { serveConn(ctx, srv, fuzzHandler{}); close(done) }()
		go func() { _, _ = io.Copy(io.Discard, cli) }() // read every reply
		_ = cli.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write([]byte(data))
		_ = cli.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("server connection did not end for input %q", data)
		}
	})
}
