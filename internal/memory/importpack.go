package memory

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"herdr-dots/internal/textutil"
)

// ImportKinds are the kinds a dot pack may carry. Preferences are personal to
// one developer and never travel; global memory is never exported.
var ImportKinds = []Kind{KindConvention, KindDecision, KindFinding, KindDismissed, KindNote}

// MaxPendingImports caps the imported entries waiting for the developer in
// one scope file (project scope, or one dot's scope), as MaxPendingProposals
// caps the agent's: a pack can't bury the developer in reviews, and since an
// import never pushes out local memory, it bounds how far imports can take a
// full scope over its cap.
const MaxPendingImports = 50

// ImportPack stores the memory entries of an imported dot pack for project p
// (Phase 9, spec S10 as amended by Plan decision 26). Every entry becomes
// someone else's conclusion:
//   - Source import, no id, no evidence, never pinned;
//   - every entry, dot or project scope, lands proposed (even with
//     AutoApproveProject) and attributed to dot; none reaches a prompt or the
//     suppression check until the developer approves it;
//   - only ImportKinds, only dot and project scope;
//   - text or paths that look like a secret are refused, never stored (the
//     scan sees the sanitized text, which is what would be stored);
//   - at most MaxPendingImports imports wait per scope file;
//   - an import never archives local memory to make room: only older imports
//     (enforceCap), and never an active entry.
//
// Per-entry problems go to Rejected (reasons never quote the text); the error
// is only for store failures. Dedupe works as for any add: an entry that
// repeats an existing one bumps it and never rewrites the developer's words,
// nor approved memory (insert).
func (s *Store) ImportPack(p, dot string, entries []Entry) (ApplyResult, error) {
	var res ApplyResult
	if !ValidProjectID(p) {
		return res, fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if !ValidSlug(dot) {
		return res, fmt.Errorf("%w: dot %q", ErrInvalid, dot)
	}
	for i, e := range entries {
		// Sanitize first, then scan, as Apply and Propose do: a control byte
		// inside a key must not hide it from the scanner and then vanish when
		// the text is stored.
		text := strings.TrimSpace(textutil.SanitizeModelText(e.Text))
		paths := sanitizePaths(e.Paths)
		why := ""
		switch {
		case e.Scope != ScopeDot && e.Scope != ScopeProject:
			why = fmt.Sprintf("scope %q can't be imported", clipRunes(stripControls(string(e.Scope)), 20))
		case !slices.Contains(ImportKinds, e.Kind):
			why = fmt.Sprintf("kind %q can't be imported", clipRunes(stripControls(string(e.Kind)), 20))
		default:
			if found, kind := ScanSecrets(text + " " + strings.Join(paths, " ")); found {
				why = "looks like a secret (" + kind + ")"
			}
		}
		if why != "" {
			res.Rejected = append(res.Rejected, fmt.Sprintf("entry %d: %s", i+1, why))
			continue
		}
		in := Entry{Scope: e.Scope, Kind: e.Kind, Text: text, Paths: paths, Dot: dot, Status: StatusProposed, Source: SourceImport}
		var r insertResult
		err := s.mutate(p, in.Scope, dotFor(in.Scope, dot), func(f *scopeFile) error {
			prep, err := s.prepare(in)
			if err != nil {
				return err
			}
			// Only a new entry needs a free slot; a repeat stores nothing new.
			if j, _ := findMatch(f, prep); j < 0 {
				if n := pendingImports(f); n >= MaxPendingImports {
					return fmt.Errorf("%d imported entries are waiting for the developer; %w", n, ErrTooManyProposals)
				}
			}
			_, r, err = s.insert(f, in)
			return err
		})
		switch {
		case err == nil && r == merged:
			res.Updated++
		case err == nil:
			res.Added++
		case errors.Is(err, ErrLocked), errors.Is(err, ErrVersion):
			return res, err
		default:
			res.Rejected = append(res.Rejected, fmt.Sprintf("entry %d: %s", i+1, reason(err)))
		}
	}
	return res, nil
}

// pendingImports counts the imported entries in f still waiting for review.
func pendingImports(f *scopeFile) int {
	n := 0
	for _, x := range f.Entries {
		if x.Status == StatusProposed && x.Source == SourceImport {
			n++
		}
	}
	return n
}

// reason names a store error without quoting entry text.
func reason(err error) string {
	if errors.Is(err, ErrTooManyProposals) {
		return err.Error() // a count and a fixed phrase, never entry text
	}
	for _, known := range []error{ErrEmpty, ErrTooLong, ErrInvalid} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "could not be stored"
}
