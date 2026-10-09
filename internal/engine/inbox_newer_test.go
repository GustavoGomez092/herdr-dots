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
// inbox.json is dropped (ruling P10-R4), never silently: the engine logs its
// severity and sanitized first line, so a paid finding can be recovered from
// engine.log, tells the developer, and leaves the newer inbox as it was.
func TestScheduledResultRefusedByANewerInboxIsDroppedWithItsFindingLogged(t *testing.T) {
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
	r.be.set(backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "Commit 3f2a skips the \x1b[31mCSRF check.\nSecond line stays out."}})
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
	if !strings.Contains(all, "warn: Commit 3f2a skips the [31mCSRF check.") {
		t.Fatalf("the dropped finding's severity and first line must be logged: %s", all)
	}
	if strings.Contains(all, "\x1b") || strings.Contains(all, "Second line") {
		t.Fatalf("the logged finding must be sanitized and one line: %q", all)
	}
	if toasts := r.toasts(); len(toasts) != 1 || !strings.Contains(toasts[0], "newer herdr-dots") || !strings.Contains(toasts[0], "upgrade") {
		t.Fatalf("toasts %v", toasts)
	}
}

// Every inbox claim (each tick, each pane) fails against a newer inbox.json:
// the refusal is logged once per process, not every tick.
func TestNewerInboxClaimRefusalIsLoggedOnce(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	var mu sync.Mutex
	var logs []string
	r := newProRig(t, proOpts{deps: func(dp *Deps) {
		dp.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	}}, d)
	r.attach(t, d.Slug)
	p := filepath.Join(r.dir, "inbox.json")
	if err := os.WriteFile(p, []byte(`{"version":2,"next":0,"cards":[],"lanes":["a"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		r.e.claimInbox(pane, testProject)
	}
	mu.Lock()
	n := 0
	for _, l := range logs {
		if strings.Contains(l, "inbox.json") && strings.Contains(l, "newer herdr-dots") {
			n++
		}
	}
	all := strings.Join(logs, "\n")
	mu.Unlock()
	if n != 1 {
		t.Fatalf("want the refusal logged once, got %d:\n%s", n, all)
	}
}
