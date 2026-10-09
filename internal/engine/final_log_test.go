package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/transcript"
)

// Final review item 11: provider text in a run error (a refusal) never puts
// control characters into the engine log.
func TestRunFailedLogLineIsSanitized(t *testing.T) {
	be := &costBE{err: errors.New("refused (I can't\x1b]0;pwned\x07 help)")}
	var logMu sync.Mutex
	var logs []string
	r := newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return be, nil }
		d.Logf = func(f string, a ...any) { logMu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); logMu.Unlock() }
	}, apiDot("Security"))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "x"})
	eventually(t, "failure logged", func() bool {
		logMu.Lock()
		defer logMu.Unlock()
		for _, l := range logs {
			if strings.Contains(l, "pwned") {
				return true
			}
		}
		return false
	})
	logMu.Lock()
	defer logMu.Unlock()
	for _, l := range logs {
		if strings.ContainsAny(l, "\x1b\x07") {
			t.Fatalf("control characters in the engine log: %q", l)
		}
	}
}
