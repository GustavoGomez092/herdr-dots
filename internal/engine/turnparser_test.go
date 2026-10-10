package engine

import (
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// T1 (post-rc hardening fix): an attachment saved before the transcript
// parser stopped counting compact-summary and slash-command records as turns
// can hold a LastSeenTurn above the new turn count. The first pass after the
// upgrade runs on the idle event of the next real turn, and that turn is
// reviewed, not absorbed.
func TestAnOldAttachmentAboveTheTurnCountReviewsTheNextRealTurn(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	for _, u := range []string{"one", "two", "three"} {
		r.readers["s1"].add(transcript.Turn{User: u})
	}
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "the catch-up review", func() bool { return r.be.n() == 1 })
	k := key{pane, "security"}
	eventually(t, "the catch-up run ends", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return !r.e.running[k]
	})

	// What an older engine saved: five turns seen under the old parser, no
	// parser version (the field is new, so the file reads it as 0).
	if err := r.atts.Update(pane, func(a *state.Attachment) {
		da := a.Find("security")
		*da = state.DotAttachment{Slug: da.Slug, LastSeenTurn: 5, MemorySeenTurn: 3}
	}); err != nil {
		t.Fatal(err)
	}

	r.turnEnds(t, transcript.Turn{User: "four"})
	eventually(t, "the next real turn is reviewed", func() bool {
		if r.be.n() != 2 {
			return false
		}
		ts := r.be.last().Turns
		return len(ts) > 0 && ts[len(ts)-1].User == "four"
	})
	a, _ := r.atts.Get(pane)
	if da := a.Find("security"); da.LastSeenTurn != 4 || da.TurnParser != transcript.ParserVersion {
		t.Fatalf("after the upgrade: %+v, want LastSeenTurn 4 and parser %d", *da, transcript.ParserVersion)
	}
}

// A new attachment carries the current parser version, so the upgrade step
// never touches it.
func TestANewAttachmentCarriesTheCurrentParserVersion(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	a, _ := r.atts.Get(pane)
	if da := a.Find("security"); da == nil || da.TurnParser != transcript.ParserVersion {
		t.Fatalf("new attachment %+v, want parser %d", da, transcript.ParserVersion)
	}
}
