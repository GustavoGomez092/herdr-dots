package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/platform"
)

// ErrEngineDown: the engine can't be reached even after starting it.
var ErrEngineDown = errors.New("dots engine not reachable; open Herdr's Dots pane (prefix+a) to start it")

type ipcEngine struct {
	c      ipc.Client
	paneID string
	ensure func() error

	mu       sync.Mutex
	ready    bool
	starting chan struct{} // non-nil while ensure runs; closed when it ends
	lastErr  error         // result of the last ensure
}

// NewIPCEngine talks to the engine over its socket, scoped to paneID. ensure
// starts the engine; it runs on the first call (never at server start, which
// must stay fast) and again once when a call can't connect. Callers wait for a
// start in progress but stop waiting when their context ends.
func NewIPCEngine(c ipc.Client, paneID string, ensure func() error) EngineAPI {
	return &ipcEngine{c: c, paneID: paneID, ensure: ensure}
}

// up makes sure the engine has been started. Concurrent callers share one
// start (singleflight); the lock is never held while ensure runs.
func (e *ipcEngine) up(ctx context.Context, force bool) error {
	e.mu.Lock()
	if e.ready && !force {
		e.mu.Unlock()
		return nil
	}
	ch := e.starting
	if ch == nil {
		ch = make(chan struct{})
		e.starting = ch
		e.ready = false
		go func() {
			var err error
			if e.ensure != nil {
				err = e.ensure()
			}
			e.mu.Lock()
			e.ready = err == nil
			e.lastErr = err
			e.starting = nil
			e.mu.Unlock()
			close(ch)
		}()
	}
	e.mu.Unlock()
	select {
	case <-ch:
	case <-ctx.Done():
		return ctx.Err()
	}
	e.mu.Lock()
	err := e.lastErr
	e.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrEngineDown, err)
	}
	return nil
}

func (e *ipcEngine) query(ctx context.Context, cmd string, args, out any) error {
	if err := e.up(ctx, false); err != nil {
		return err
	}
	err := e.c.Query(ctx, cmd, args, out)
	if !dialFailed(ctx, err) {
		return err
	}
	if err := e.up(ctx, true); err != nil {
		return err
	}
	err = e.c.Query(ctx, cmd, args, out)
	if dialFailed(ctx, err) {
		return ErrEngineDown
	}
	return err
}

// dialFailed reports a failed dial. A dial that failed because the caller's
// own context ended says nothing about the engine. A unix socket dial fails
// with *net.OpError{Op: "dial"}; the Windows engine endpoint is a named pipe,
// whose dial (go-winio, ipc.Dial) fails with *os.PathError{Op: "open"} on the
// pipe's path (no such pipe: the engine is gone). A pipe that opened but
// belongs to someone else (platform.ErrNotOwner) is not a failed dial.
func dialFailed(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var pe *os.PathError
	return errors.As(err, &pe) && pe.Op == "open" && strings.HasPrefix(strings.ToLower(pe.Path), platform.PipePrefix)
}

func (e *ipcEngine) ListDots(ctx context.Context) (ipc.DotsResult, error) {
	var r ipc.DotsResult
	err := e.query(ctx, ipc.CmdListDots, ipc.PaneArgs{PaneID: e.paneID}, &r)
	return r, err
}

func (e *ipcEngine) Ask(ctx context.Context, slug, question string, includeTurns bool) (ipc.AskResult, error) {
	var r ipc.AskResult
	err := e.query(ctx, ipc.CmdAsk, ipc.AskArgs{PaneID: e.paneID, Slug: slug, Question: question, IncludeTurns: includeTurns}, &r)
	return r, err
}

func (e *ipcEngine) AskCouncil(ctx context.Context, question string) (ipc.CouncilResult, error) {
	var r ipc.CouncilResult
	err := e.query(ctx, ipc.CmdAskCouncil, ipc.AskCouncilArgs{PaneID: e.paneID, Question: question}, &r)
	return r, err
}

func (e *ipcEngine) ListQueue(ctx context.Context, status string) (ipc.QueueResult, error) {
	var r ipc.QueueResult
	err := e.query(ctx, ipc.CmdListQueue, ipc.ListQueueArgs{PaneID: e.paneID, Status: status}, &r)
	return r, err
}

func (e *ipcEngine) MemoryChanged(ctx context.Context) error {
	if err := e.up(ctx, false); err != nil {
		return err
	}
	return e.c.Call(ctx, ipc.CmdMemoryChanged, nil)
}
