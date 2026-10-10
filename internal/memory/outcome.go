package memory

import (
	"fmt"
	"strings"

	"herdr-dots/internal/textutil"
)

// redacted replaces ledger text that looked like it held a secret.
const redacted = "[possible secret removed]"

// appliedPrefix starts the finding an applied patch card leaves.
const appliedPrefix = "Applied patch: "

// Patch card outcomes (Phase 7).
const (
	OutcomeApplied   = "applied"   // applied to the working tree: a finding "Applied patch: …"
	OutcomeDiscarded = "discarded" // thrown away: a ledger line only, no entry
)

// RecordOutcome appends a card outcome to the project's ledger and derives a
// dot-scope entry with no model call (spec §6.4): sent → finding from the
// card's first sentence, edited → finding from the edited text, dismissed →
// dismissed, applied (a patch card) → finding "Applied patch: <first
// sentence>", discarded (a patch card) → ledger line only: throwing a draft
// away says nothing about whether the advice was right. Cards that look like
// they quote a secret leave a redacted ledger line and no entry. Card text
// came from a model, so it is sanitized first.
func (s *Store) RecordOutcome(p string, o Outcome) error {
	if !ValidProjectID(p) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if !ValidSlug(o.Dot) {
		return fmt.Errorf("%w: dot %q", ErrInvalid, o.Dot)
	}
	o.Body = textutil.SanitizeModelText(o.Body)
	o.Edited = textutil.SanitizeModelText(o.Edited)
	var kind Kind
	text := FirstSentence(o.Body)
	switch o.Kind {
	case OutcomeSent:
		kind = KindFinding
	case OutcomeEdited:
		kind, text = KindFinding, FirstSentence(o.Edited)
	case OutcomeDismissed:
		kind = KindDismissed
	case OutcomeApplied:
		kind = KindFinding
		if text != "" {
			text = clipRunes(appliedPrefix+text, 300)
		}
	case OutcomeDiscarded:
		text = "" // ledger only
	default:
		return fmt.Errorf("%w: outcome %q", ErrInvalid, o.Kind)
	}
	if o.At.IsZero() {
		o.At = s.now()
	}
	secret := o.Secret
	for _, t := range []string{o.Body, o.Edited} {
		if found, _ := ScanSecrets(t); found {
			secret = true
		}
	}
	if secret {
		o.Body = redacted
		if o.Edited != "" {
			o.Edited = redacted
		}
	}
	o.Body, o.Edited = clipRunes(o.Body, 300), clipRunes(o.Edited, 300)
	// The ledger line and its entry are written under one hold of the
	// project lock (the dot file lives in the project directory), so no
	// other writer runs between them (M25).
	return s.withLock(s.dirFor(p, ScopeProject), func() error {
		if err := s.appendLedgerLocked(p, o); err != nil {
			return err
		}
		if secret {
			s.logf("memory: card %s looked like it quoted a secret; no memory entry created", o.Card)
			return nil
		}
		if text == "" {
			return nil
		}
		e := Entry{Scope: ScopeDot, Dot: o.Dot, Kind: kind, Text: text, Source: SourceOutcome,
			Evidence: Evidence{Session: o.Session, Pane: o.Pane, Turn: o.Turn, Card: o.Card}}
		return s.mutateLocked(p, ScopeDot, o.Dot, func(f *scopeFile) error {
			_, _, err := s.insert(f, e)
			return err
		})
	})
}

// RememberInput is the Dots pane's "remember this card" request.
type RememberInput struct {
	As     RememberAs
	Dot    string // the card's dot
	Text   string // the card body
	Reason string // optional, for AsRule
	Ev     Evidence
}

// Remember stores a card the developer chose to remember (spec §7.2): an
// accepted finding, a "don't raise this again" rule, a project convention
// (active right away), or a global preference for the dot. Text that looks
// like a secret is refused with ErrSecret.
func (s *Store) Remember(p string, in RememberInput) (Entry, error) {
	text := FirstSentence(in.Text)
	reason := strings.Join(strings.Fields(in.Reason), " ")
	e := Entry{Dot: in.Dot, Source: SourceUser, Status: StatusActive, Evidence: in.Ev}
	switch in.As {
	case AsFinding:
		e.Scope, e.Kind = ScopeDot, KindFinding
	case AsRule:
		e.Scope, e.Kind = ScopeDot, KindDismissed
		if reason != "" {
			text += ReasonSep + reason + ")"
		}
	case AsConvention:
		e.Scope, e.Kind, e.Dot = ScopeProject, KindConvention, ""
	case AsGlobal:
		e.Scope, e.Kind = ScopeGlobal, KindPreference
	default:
		return Entry{}, fmt.Errorf("%w: remember as %q", ErrInvalid, in.As)
	}
	if found, kind := ScanSecrets(text); found {
		return Entry{}, fmt.Errorf("%w (%s): use `herdr-dots memory add --force` if you are sure", ErrSecret, kind)
	}
	e.Text = clipRunes(text, s.maxText())
	return s.Add(p, e)
}
