package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

// noKeyAtRest fails if any file in the patch store holds key.
func noKeyAtRest(t *testing.T, dir, key string) {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil && strings.Contains(string(b), key) {
			t.Fatalf("%s holds the key at rest:\n%s", e.Name(), b)
		}
	}
}

// A dot's summary that quotes a key is never stored unredacted: the record
// only says the summary had a secret, and memory still gets the redacted
// ledger line and no entry.
func TestADraftSummarySecretIsNeverStoredAndMemoryStillSkipsIt(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	key := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	r.dr.mu.Lock()
	r.dr.msg = "Rotated " + key + " out of the config."
	r.dr.mu.Unlock()
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	noKeyAtRest(t, r.patches.Dir, key)
	m, err := r.patches.Get(c.PatchID)
	if err != nil || !m.SummaryHadSecret || strings.Contains(m.Summary, key) || m.Summary != c.Body {
		t.Fatalf("meta %+v %v", m, err)
	}
	if err := r.applyReviewed(c.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the applied ledger line", func() bool {
		l, _ := r.mem.Ledger(testProject)
		for _, o := range l {
			if o.Kind == memory.OutcomeApplied {
				return true
			}
		}
		return false
	})
	l, _ := r.mem.Ledger(testProject)
	for _, o := range l {
		if o.Kind == memory.OutcomeApplied && o.Body != "[possible secret removed]" {
			t.Fatalf("ledger line %+v, want the redacted marker", o)
		}
	}
	if es, _ := r.mem.List(testProject, memory.Filter{Scope: memory.ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("an entry from a summary that held a secret: %+v", es)
	}
	noKeyAtRest(t, r.patches.Dir, key)
}

// A clean summary still makes the usual "Applied patch: …" finding.
func TestACleanDraftSummaryStillTeachesMemory(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	if m, _ := r.patches.Get(c.PatchID); m.SummaryHadSecret {
		t.Fatalf("clean summary flagged: %+v", m)
	}
	if err := r.applyReviewed(c.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "an Applied patch finding", func() bool {
		es, _ := r.mem.List(testProject, memory.Filter{Scope: memory.ScopeDot, Dot: "security"})
		return len(es) == 1 && strings.HasPrefix(es[0].Text, "Applied patch: Moved the JWT")
	})
}
