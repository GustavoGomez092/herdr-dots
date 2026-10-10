package delivery

import (
	"context"
	"errors"
	"testing"
)

// L15 (post-rc hardening, test-only): sending no cards asks nothing of the
// pane and sends nothing, even when the pane can't be read.
func TestSendWithNoCardsTouchesNothing(t *testing.T) {
	f := &fakeTarget{getErr: errors.New("pane gone")}
	if g, err := Send(context.Background(), f, "w1:p1", nil, true); g != Now || err != nil || len(f.sent) != 0 {
		t.Fatalf("gate %v err %v sent %q", g, err, f.sent)
	}
}
