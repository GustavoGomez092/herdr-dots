package transcript

import (
	"context"
	"strings"
	"sync"
)

type PaneReader interface {
	PaneRead(ctx context.Context, paneID, source string, lines int) (string, error)
}

// ScrollbackReader is the lossy fallback: each read that changed the screen
// becomes one turn holding the new text since the previous read.
type ScrollbackReader struct {
	pane   PaneReader
	paneID string
	lines  int

	mu    sync.Mutex
	last  string
	turns []Turn
}

func NewScrollback(p PaneReader, paneID string, lines int) *ScrollbackReader {
	return &ScrollbackReader{pane: p, paneID: paneID, lines: lines}
}

func (r *ScrollbackReader) Source() string { return SourceScrollback }

func (r *ScrollbackReader) Turns(ctx context.Context) ([]Turn, error) {
	text, err := r.pane.PaneRead(ctx, r.paneID, "recent_unwrapped", r.lines)
	if err != nil {
		return nil, err
	}
	text = strings.TrimRight(text, " \n")
	r.mu.Lock()
	defer r.mu.Unlock()
	if text != r.last {
		delta := newTail(r.last, text)
		r.last = text
		if strings.TrimSpace(delta) != "" {
			r.turns = append(r.turns, Turn{Index: len(r.turns) + 1, Assistant: delta, FromDots: FromDots(delta)})
		}
	}
	return append([]Turn(nil), r.turns...), nil
}

// newTail returns the part of cur after the last few lines of old.
func newTail(old, cur string) string {
	if old == "" {
		return cur
	}
	lines := strings.Split(old, "\n")
	n := min(5, len(lines))
	anchor := strings.Join(lines[len(lines)-n:], "\n")
	if i := strings.LastIndex(cur, anchor); i >= 0 {
		return strings.TrimLeft(cur[i+len(anchor):], "\n")
	}
	return cur
}
