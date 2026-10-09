package memory

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"herdr-dots/internal/textutil"
)

// SourceAgent marks project memory the main coding agent proposed through the
// Dots MCP server (Phase 5).
const SourceAgent Source = "agent"

// AgentKinds are the kinds the main agent may propose.
var AgentKinds = []Kind{KindConvention, KindDecision, KindNote}

// Propose stores a project-scope proposal from the main agent. Whatever the
// caller set, the entry is project scope, status proposed (the developer
// approves it in the memory screen), source agent, no dot, not pinned.
// Secret-looking text is refused with ErrSecret. Text matching an existing
// entry returns that entry (its uses go up) with its own status.
func (s *Store) Propose(p string, e Entry) (Entry, error) {
	if !ValidProjectID(p) {
		return Entry{}, fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if !slices.Contains(AgentKinds, e.Kind) {
		return Entry{}, fmt.Errorf("%w: kind %q (want convention, decision or note)", ErrInvalid, e.Kind)
	}
	// Sanitize first, then scan, as Apply does: a control byte inside a key
	// must not hide it from the scanner and then vanish when the text is stored.
	e.Text = strings.TrimSpace(textutil.SanitizeModelText(e.Text))
	if found, kind := ScanSecrets(e.Text); found {
		return Entry{}, fmt.Errorf("%w (%s)", ErrSecret, kind)
	}
	for _, p := range e.Paths {
		if found, kind := ScanSecrets(stripControls(p)); found {
			return Entry{}, fmt.Errorf("%w (%s, in a path)", ErrSecret, kind)
		}
	}
	for _, v := range []string{e.Evidence.Session, e.Evidence.Pane, e.Evidence.Card} {
		if found, kind := ScanSecrets(stripControls(v)); found {
			return Entry{}, fmt.Errorf("%w (%s, in the evidence)", ErrSecret, kind)
		}
	}
	return s.Add(p, Entry{Scope: ScopeProject, Kind: e.Kind, Text: e.Text, Paths: e.Paths,
		Status: StatusProposed, Source: SourceAgent, Evidence: e.Evidence})
}

func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
