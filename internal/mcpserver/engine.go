package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"herdr-dots/internal/ipc"
)

// ErrEngineDown: the engine can't be reached even after starting it.
var ErrEngineDown = errors.New("dots engine not reachable; open Herdr's Dots pane (prefix+a) to start it")

type ipcEngine struct {
	c      ipc.Client
	paneID string
	ensure func() error

	mu    sync.Mutex
	ready bool
}

// NewIPCEngine talks to the engine over its socket, scoped to paneID. ensure
// starts the engine; it runs on the first call (never at server start, which
// must stay fast) and again once when a call can't connect.
func NewIPCEngine(c ipc.Client, paneID string, ensure func() error) EngineAPI {
	return &ipcEngine{c: c, paneID: paneID, ensure: ensure}
}

func (e *ipcEngine) up(force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ready && !force {
		return nil
	}
	if e.ensure != nil {
		if err := e.ensure(); err != nil {
			return fmt.Errorf("%w (%v)", ErrEngineDown, err)
		}
	}
	e.ready = true
	return nil
}

func (e *ipcEngine) query(ctx context.Context, cmd string, args, out any) error {
	if err := e.up(false); err != nil {
		return err
	}
	err := e.c.Query(ctx, cmd, args, out)
	if !dialFailed(err) {
		return err
	}
	if err := e.up(true); err != nil {
		return err
	}
	err = e.c.Query(ctx, cmd, args, out)
	if dialFailed(err) {
		return ErrEngineDown
	}
	return err
}

func dialFailed(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
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
	if err := e.up(false); err != nil {
		return err
	}
	return e.c.Call(ctx, ipc.CmdMemoryChanged, nil)
}
