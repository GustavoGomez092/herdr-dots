// Package ipc is the newline-delimited JSON protocol between the engine and
// the Bubble Tea UIs over the engine's local endpoint: a unix socket, or on
// Windows a named pipe only the current user can open.
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"herdr-dots/internal/view"
)

const (
	CmdPing       = "ping"
	CmdWatch      = "watch"
	CmdAttach     = "attach"
	CmdDetach     = "detach"
	CmdSend       = "send"
	CmdDismiss    = "dismiss"
	CmdEdit       = "edit"
	CmdSummon     = "summon"
	CmdSetMode    = "set_mode"
	CmdPause      = "pause"
	CmdPauseAll   = "pause_all"
	CmdReset      = "reset"
	CmdReloadDots = "reload_dots"

	CmdRemember      = "remember"       // RememberArgs: turn a card into a memory entry
	CmdMemoryChanged = "memory_changed" // no args: a UI edited memory; refresh snapshot counts
)

// RememberArgs.As is finding, rule, convention or global.
type RememberArgs struct {
	CardID string `json:"card_id"`
	As     string `json:"as"`
	Reason string `json:"reason,omitempty"`
}

// Council commands (Phase 3).
const (
	CmdCouncil          = "council"           // CouncilArgs: convene a council (runs in the background)
	CmdCouncilPreflight = "council_preflight" // CouncilArgs: publish a preflight in the pane's snapshot
)

// CouncilArgs convenes (or preflights) a council on a main pane. Empty Slugs
// means every eligible attached dot.
type CouncilArgs struct {
	PaneID   string   `json:"pane_id"`
	Question string   `json:"question,omitempty"`
	Slugs    []string `json:"slugs,omitempty"`
	Request  string   `json:"request,omitempty"` // council_preflight: the popup's id, echoed in Preflight.Request
}

// Draft commands (Phase 7). Only the Dots pane and the diff viewer send
// them, after the user confirmed; they are never queries, so the MCP server
// can't reach them.
const (
	CmdDraft        = "draft"         // DraftArgs: start a draft (gate 1 was confirmed)
	CmdApplyPatch   = "apply_patch"   // CardArgs: apply a patch card to the working tree (gate 2 was confirmed)
	CmdDiscardPatch = "discard_patch" // CardArgs: delete a patch card's patch, metadata and snapshot ref
)

// DraftArgs starts a draft by Slug on PaneID. Instruction "" with a
// FromCardID uses that card's body.
type DraftArgs struct {
	PaneID      string `json:"pane_id"`
	Slug        string `json:"slug"`
	Instruction string `json:"instruction,omitempty"`
	FromCardID  string `json:"from_card_id,omitempty"`
}

type AttachArgs struct {
	PaneID string   `json:"pane_id"`
	Slugs  []string `json:"slugs"`
}

type DotArgs struct {
	PaneID string `json:"pane_id"`
	Slug   string `json:"slug"`
}

type SendArgs struct {
	IDs []string `json:"ids"`
	// Parts is, for each digest in IDs, how many parts the sender showed. The
	// engine refuses a digest whose count differs or is missing.
	Parts map[string]int `json:"parts,omitempty"`
}

type CardArgs struct {
	ID   string `json:"id"`
	Body string `json:"body,omitempty"`
	// SHA256 (hex) binds CmdApplyPatch to the patch bytes the user saw; the
	// engine refuses an apply whose hash isn't the card's current patch.
	SHA256 string `json:"sha256,omitempty"`
}

type SummonArgs struct {
	PaneID   string `json:"pane_id"`
	Slug     string `json:"slug"`
	Question string `json:"question,omitempty"`
}

type SetModeArgs struct {
	Slug string `json:"slug"`
	Mode string `json:"mode"`
}

type PauseArgs struct {
	PaneID string `json:"pane_id"`
	Slug   string `json:"slug"`
	Paused bool   `json:"paused"`
}

type Request struct {
	ID   int             `json:"id"`
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
}

type Response struct {
	ID       int             `json:"id"`
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Snapshot *view.Snapshot  `json:"snapshot,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"` // query commands only (query.go)
}

type Handler interface {
	Handle(ctx context.Context, cmd string, args json.RawMessage) error
	Snapshot() view.Snapshot
	Watch() (<-chan view.Snapshot, func())
}

// Serve accepts connections until ctx ends or the listener fails. On the way
// out it closes every open connection and waits for all connection goroutines
// (so no handler or query is still running when Serve returns).
func Serve(ctx context.Context, ln net.Listener, h Handler) error {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		conns  = map[net.Conn]struct{}{}
		closed bool
	)
	go func() {
		<-ctx.Done()
		ln.Close()
		mu.Lock()
		closed = true
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
	}()
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		mu.Lock()
		if closed {
			mu.Unlock()
			conn.Close()
			continue
		}
		conns[conn] = struct{}{}
		wg.Add(1)
		mu.Unlock()
		go func() {
			defer wg.Done()
			defer func() { mu.Lock(); delete(conns, conn); mu.Unlock() }()
			serveConn(ctx, conn, h)
		}()
	}
}

// limitedReader allows at most n more bytes until reset; past that it reports
// EOF, which drops the connection.
type limitedReader struct {
	r io.Reader
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, io.EOF
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}

func serveConn(ctx context.Context, conn net.Conn, h Handler) {
	defer conn.Close()
	lr := &limitedReader{r: conn}
	dec := json.NewDecoder(lr)
	enc := json.NewEncoder(conn)
	for {
		lr.n = maxRequestBytes
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.Cmd == CmdWatch {
			stream(ctx, conn, enc, req.ID, h)
			return
		}
		if isQuery(req.Cmd) {
			serveQuery(ctx, conn, dec, enc, req, h) // one query per connection
			return
		}
		resp := Response{ID: req.ID, OK: true}
		if err := h.Handle(ctx, req.Cmd, req.Args); err != nil {
			resp.OK, resp.Error = false, err.Error()
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func stream(ctx context.Context, conn net.Conn, enc *json.Encoder, id int, h Handler) {
	ch, stop := h.Watch()
	defer stop()
	s := h.Snapshot()
	if enc.Encode(Response{ID: id, OK: true, Snapshot: &s}) != nil {
		return
	}
	gone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, conn); close(gone) }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-gone:
			return
		case s := <-ch:
			if enc.Encode(Response{ID: id, OK: true, Snapshot: &s}) != nil {
				return
			}
		}
	}
}

// callTimeout is Call's deadline when its context has none. Query has no
// default: a dot run can take longer (Phase 5).
var callTimeout = 30 * time.Second

// maxRequestBytes bounds one request line read by the server.
const maxRequestBytes = 1 << 20

// Client talks to the engine. Path is the engine endpoint
// (paths.Paths.EngineEndpoint).
type Client struct{ Path string }

func (c Client) dial(ctx context.Context) (net.Conn, error) { return Dial(ctx, c.Path) }

func (c Client) Call(ctx context.Context, cmd string, args any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(callTimeout)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	var raw json.RawMessage
	if args != nil {
		if raw, err = json.Marshal(args); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Cmd: cmd, Args: raw}); err != nil {
		return err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

// Watch streams snapshots (latest wins) until ctx ends or the engine goes
// away; the channel is then closed.
func (c Client) Watch(ctx context.Context) (<-chan view.Snapshot, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Cmd: CmdWatch}); err != nil {
		conn.Close()
		return nil, err
	}
	ch := make(chan view.Snapshot, 1)
	go func() { <-ctx.Done(); conn.Close() }()
	go func() {
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			var r Response
			if err := dec.Decode(&r); err != nil {
				return
			}
			if r.Snapshot == nil {
				continue
			}
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- *r.Snapshot:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
