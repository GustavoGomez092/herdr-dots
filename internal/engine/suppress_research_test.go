package engine

import (
	"testing"

	"herdr-dots/internal/memory"
)

// Phase 2 (vital): only outcome and user findings and dismissed entries
// suppress repeats. A research or question entry never does, even when it is
// word for word the new card.
func TestSuppressedByIgnoresResearchAndQuestionEntries(t *testing.T) {
	ev := memory.Evidence{Session: "s1", Turn: 1}
	for _, k := range memory.ResearchKinds {
		for _, src := range []memory.Source{memory.SourceModel, memory.SourceOutcome, memory.SourceUser} {
			mr := memRun{cands: []memory.Entry{{ID: "m1", Scope: memory.ScopeDot, Dot: "security", Kind: k,
				Text: jwtFirst.Message, Source: src, Evidence: ev}}}
			if ref, ok := suppressedBy(mr, jwtFirst.Message, 2, "s1"); ok {
				t.Errorf("a %s entry (%s) suppressed a card: %s", k, src, ref)
			}
		}
	}
}
