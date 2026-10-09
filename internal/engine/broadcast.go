package engine

import (
	"sync"
	"time"

	"herdr-dots/internal/view"
)

// minBroadcastGap spaces snapshot broadcasts: at most 10 a second (spec F9).
const minBroadcastGap = 100 * time.Millisecond

// throttle is the broadcast state. Its mutex is a leaf lock: nothing else is
// taken and no I/O is done while it is held (sendSnapshot takes it under
// e.mu, never the other way round).
type throttle struct {
	mu      sync.Mutex
	pending bool      // a trailing broadcast is scheduled
	last    time.Time // when the last broadcast was sent
	seq     uint64    // sends started: each sendSnapshot takes the next number before it builds
	sent    uint64    // the newest send handed to the watchers

	built func() // tests only (set before Start): called between building and handing over a snapshot
}

// Snapshot is the engine state for the UIs, trimmed to the snapshot budget.
func (e *Engine) Snapshot() view.Snapshot {
	return view.Trim(e.buildSnapshot(), view.MaxSnapshotBytes)
}

// broadcast tells every watcher about a change. The first change after a
// quiet gap is sent at once; changes within minBroadcastGap of a broadcast
// are coalesced into one trailing broadcast that carries the final state, so
// no change waits longer than minBroadcastGap (plus building the snapshot).
func (e *Engine) broadcast() {
	e.bc.mu.Lock()
	if e.bc.pending {
		e.bc.mu.Unlock()
		return
	}
	now := time.Now()
	wait := e.bc.last.Add(minBroadcastGap).Sub(now)
	if wait <= 0 {
		e.bc.last = now
		e.bc.mu.Unlock()
		e.sendSnapshot()
		return
	}
	e.bc.pending = true
	e.bc.mu.Unlock()
	if !e.spawn(func() {
		time.Sleep(wait)
		e.bc.mu.Lock()
		e.bc.pending = false
		e.bc.last = time.Now()
		e.bc.mu.Unlock()
		e.sendSnapshot()
	}) {
		// The engine is stopping (Wait ran): no trailing broadcast.
		e.bc.mu.Lock()
		e.bc.pending = false
		e.bc.mu.Unlock()
	}
}

// sendSnapshot hands the current snapshot to every watcher, replacing one
// it hasn't read yet. Two sends can overlap (one still building when the
// trailing one fires): a send whose number is older than one already handed
// over is dropped, so watchers never go back to a staler state. The newest
// send always lands, and it began after every change that asked for it.
func (e *Engine) sendSnapshot() {
	e.bc.mu.Lock()
	e.bc.seq++
	seq := e.bc.seq
	e.bc.mu.Unlock()
	s := e.Snapshot()
	if e.bc.built != nil {
		e.bc.built()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bc.mu.Lock()
	stale := seq < e.bc.sent
	if !stale {
		e.bc.sent = seq
	}
	e.bc.mu.Unlock()
	if stale {
		return
	}
	for _, ch := range e.watchers {
		select {
		case <-ch: // drop the stale snapshot
		default:
		}
		select {
		case ch <- s:
		default:
		}
	}
}
