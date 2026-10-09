package queue

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/fsutil"
)

// A newer herdr-dots in another session can upgrade inbox.json after this
// process started (so internal/migrate never marked it). Every inbox access
// then refuses it under the lock and leaves it byte for byte: Put returns a
// clear error, and Claim leaves the cards where they are.
func TestNewerInboxIsRefusedAndNeverRewritten(t *testing.T) {
	in, q, p := inboxRig(t)
	future := `{"version":2,"next":1,"cards":[{"id":"inbox-1","dot":"security","project":"g-0123456789abcdef","body":"x","mode":"queue","status":"pending","created_at":"2026-10-08T12:00:00Z"}],"lanes":["a"]}`
	if err := os.WriteFile(p, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	err := in.Put(Card{Project: "g-0123456789abcdef", Dot: "security", Body: "y", Mode: "queue", Origin: "scheduled"})
	if !errors.Is(err, ErrInboxVersion) || !errors.Is(err, fsutil.ErrNewerVersion) || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("Put on a newer inbox: %v", err)
	}
	if n, err := q.Claim(in, "g-0123456789abcdef", "w1:p1"); n != 0 || !errors.Is(err, ErrInboxVersion) {
		t.Fatalf("Claim on a newer inbox: %d, %v", n, err)
	}
	if _, err := in.List("g-0123456789abcdef"); !errors.Is(err, ErrInboxVersion) {
		t.Fatalf("List on a newer inbox: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != future {
		t.Fatalf("newer inbox changed: %s", b)
	}
	if asides, _ := filepath.Glob(p + ".corrupt-*"); len(asides) != 0 {
		t.Fatalf("a newer inbox is not corrupt: %v", asides)
	}
	if len(q.All()) != 0 {
		t.Fatal("a card was claimed from a newer inbox")
	}
}

// What this version writes says which version it is.
func TestInboxWritesItsVersion(t *testing.T) {
	in, _, p := inboxRig(t)
	if err := in.Put(Card{Project: "g-0123456789abcdef", Dot: "security", Body: "y", Mode: "queue", Origin: "scheduled"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"version": 1`) {
		t.Fatalf("inbox.json: %s", b)
	}
}
