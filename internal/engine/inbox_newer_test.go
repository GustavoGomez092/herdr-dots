package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
)

// A scheduled result that can't be filed because a newer herdr-dots wrote
// inbox.json is never dropped silently: the engine logs it, tells the
// developer, and leaves the newer inbox exactly as it was.
func TestScheduledResultForANewerInboxIsLoggedNotDropped(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	var mu sync.Mutex
	var logs []string
	r := newProRig(t, proOpts{deps: func(dp *Deps) {
		dp.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	}}, d)
	p := filepath.Join(r.dir, "inbox.json")
	future := `{"version":2,"next":0,"cards":[],"lanes":["a"]}`
	if err := os.WriteFile(p, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	r.be.set(backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "Commit 3f2a skips the CSRF check."}})
	r.e.runBackground(scheduledJob(r, d))
	if b, _ := os.ReadFile(p); string(b) != future {
		t.Fatalf("newer inbox changed: %s", b)
	}
	mu.Lock()
	all := strings.Join(logs, "\n")
	mu.Unlock()
	if !strings.Contains(all, "not filed") || !strings.Contains(all, "newer herdr-dots") {
		t.Fatalf("logs: %s", all)
	}
	if toasts := r.toasts(); len(toasts) != 1 || !strings.Contains(toasts[0], "newer herdr-dots") || !strings.Contains(toasts[0], "upgrade") {
		t.Fatalf("toasts %v", toasts)
	}
}
