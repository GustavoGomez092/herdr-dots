package queue

import (
	"os"
	"path/filepath"
	"testing"
)

// Q2: Open doesn't trust a persisted "next" below an existing card id (a
// hand-edited or damaged file), so a new card never reuses an id.
func TestOpenNeverReusesACardID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "queue.json")
	body := `{"next": 1, "cards": [{"id": "1", "pane_id": "w1:p1", "status": "pending"}, {"id": "7", "pane_id": "w1:p1", "status": "pending"}]}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Add(Card{PaneID: "w1:p1", Body: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "8" {
		t.Fatalf("new card id %q, want 8 (after the highest existing id)", c.ID)
	}
}
