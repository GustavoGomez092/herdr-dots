package engine

import (
	"time"

	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// chairMemoryChars caps the chair's memory block.
const chairMemoryChars = 6000

// chairMemory is the chair's read-only memory (spec C12): the project's
// active conventions and decisions, so the digest can say "this conflicts
// with decision p:m3". "" when memory is off, the project is unknown or the
// store can't be read. The chair never writes memory.
func (e *Engine) chairMemory(att *state.Attachment, turns []transcript.Turn) string {
	if e.d.Memory == nil || !e.config().MemoryEnabled {
		return ""
	}
	pid := e.projectOf(att)
	if pid == "" {
		return ""
	}
	entries, err := e.d.Memory.List(pid, memory.Filter{
		Scope:    memory.ScopeProject,
		Kinds:    []memory.Kind{memory.KindConvention, memory.KindDecision},
		Statuses: []memory.Status{memory.StatusActive},
	})
	if err != nil {
		e.d.Logf("council chair memory for %s: %v", att.PaneID, err)
	}
	q := memory.Query{Paths: touchedPaths(turns), Text: turnsText(turns), Now: time.Now()}
	return memory.Render(memory.Select(entries, q, chairMemoryChars))
}

// recordCouncilOutcomes records a council card's outcome once per member that
// contributed (spec §6 step 8): sent, edited (with the edited digest) or
// dismissed. Failed members and members with nothing to add record nothing.
// Called from recordOutcome, off every engine lock.
func (e *Engine) recordCouncilOutcomes(c queue.Card, kind string, edited bool) {
	pid := c.Project
	a, attached := e.d.Attachments.Get(c.PaneID)
	if pid == "" && attached {
		pid = e.projectOf(&a)
	}
	if pid == "" {
		e.d.Logf("council card %s: project unknown; outcomes not remembered", c.ID)
		return
	}
	e.mu.Lock()
	known := make(map[string]dots.Dot, len(c.Parts))
	for _, p := range c.Parts {
		if d, ok := e.dots[p.Dot]; ok {
			known[p.Dot] = d
		}
	}
	e.mu.Unlock()
	members := councilMembers(c)
	for _, p := range c.Parts {
		body := council.OwnText(p.Body) // no "↳ ADD: " and no "## " header in memory
		if p.Failed || body == council.NoPoint {
			continue
		}
		if d, ok := known[p.Dot]; ok && !d.Memory.Enabled {
			continue
		}
		o := memory.Outcome{Dot: p.Dot, Card: c.ID, Kind: kind, Severity: p.Severity, Body: body, Turn: c.Turn, Pane: c.PaneID}
		if attached {
			o.Session = a.SessionValue
		}
		if kind == memory.OutcomeSent && edited {
			// The member's own bullet of the edited digest; a member no bullet
			// names keeps its own answer and records a plain send.
			if text, ok := council.MemberPoint(c.Body, members, council.Member{Name: p.DotName, Slug: p.Dot}, p.Body); ok {
				o.Kind, o.Edited = memory.OutcomeEdited, text
			}
		}
		if err := retryLocked(func() error { return e.d.Memory.RecordOutcome(pid, o) }); err != nil {
			e.memWriteFailed(err)
			return
		}
	}
	e.refreshStats(pid)
	e.broadcast()
}

// councilMembers is the card's members as a digest attribution may name
// them: display names and slugs.
func councilMembers(c queue.Card) []council.Member {
	out := make([]council.Member, 0, len(c.Parts))
	for _, p := range c.Parts {
		out = append(out, council.Member{Name: p.DotName, Slug: p.Dot})
	}
	return out
}

// councilConventionText is the text to remember from a council digest: its
// first bullet point, without the list marker and the member attribution
// (council.FirstPoint). Headers ("## Must fix") are never returned.
func councilConventionText(c queue.Card) string {
	return council.FirstPoint(c.Body, councilMembers(c))
}
