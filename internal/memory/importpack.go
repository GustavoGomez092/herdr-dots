package memory

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"herdr-dots/internal/textutil"
)

// ImportKinds are the kinds a dot pack may carry. Preferences are personal to
// one developer and never travel; global memory is never exported.
var ImportKinds = []Kind{KindConvention, KindDecision, KindFinding, KindDismissed, KindNote}

// MaxPendingImports caps the imported entries waiting for the developer in
// one scope file (project scope, or one dot's scope), as MaxPendingProposals
// caps the agent's: a pack can't bury the developer in reviews, and since an
// import archives nothing on arrival, it bounds how far imports can take a
// full scope over its cap.
const MaxPendingImports = 50

// ImportPack stores the memory entries of an imported dot pack for project p
// (Phase 9, spec S10 as amended by Plan decision 26). Every entry becomes
// someone else's conclusion:
//   - Source import, From dot, no id, no evidence, never pinned;
//   - every entry, dot or project scope, lands proposed (even with
//     AutoApproveProject) and attributed to dot; none reaches a prompt or the
//     suppression check until the developer approves it;
//   - only ImportKinds, only dot and project scope;
//   - text or paths that look like a secret are refused, never stored (the
//     scan sees the sanitized text, which is what would be stored);
//   - at most MaxPendingImports imports wait per scope file; the rest are
//     refused;
//   - an import archives nothing to make room (enforceCap), so Added counts
//     exactly the entries left proposed.
//
// The whole pack is written under one hold of the project's memory lock, one
// write per scope file: a lock timeout stores nothing, and a file that can't
// be written gets none of its entries.
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
	type rejection struct {
		i   int
		why string
	}
	var rejected []rejection
	type item struct {
		i  int
		in Entry
	}
	var order []Scope // scope files in order of first appearance
	batches := map[Scope][]item{}
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
			rejected = append(rejected, rejection{i, why})
			continue
		}
		if _, ok := batches[e.Scope]; !ok {
			order = append(order, e.Scope)
		}
		batches[e.Scope] = append(batches[e.Scope], item{i, Entry{Scope: e.Scope, Kind: e.Kind, Text: text, Paths: paths,
			Dot: dot, Status: StatusProposed, Source: SourceImport, From: dot}})
	}
	// Project and dot scope files share the project directory's lock.
	var err error
	if len(order) > 0 {
		err = s.withLock(s.dirFor(p, ScopeProject), func() error {
			for _, sc := range order {
				var added, updated int
				var why []rejection
				err := s.mutateLocked(p, sc, dotFor(sc, dot), func(f *scopeFile) error {
					added, updated, why = 0, 0, nil
					for _, it := range batches[sc] {
						r, err := s.importOne(f, it.in)
						switch {
						case err != nil:
							why = append(why, rejection{it.i, reason(err)})
						case r == merged:
							updated++
						default:
							added++
						}
					}
					return nil
				})
				if errors.Is(err, ErrVersion) {
					return err
				}
				if err != nil {
					for _, it := range batches[sc] {
						rejected = append(rejected, rejection{it.i, reason(err)})
					}
					continue
				}
				res.Added += added
				res.Updated += updated
				rejected = append(rejected, why...)
			}
			return nil
		})
	}
	sort.SliceStable(rejected, func(a, b int) bool { return rejected[a].i < rejected[b].i })
	for _, r := range rejected {
		res.Rejected = append(res.Rejected, fmt.Sprintf("entry %d: %s", r.i+1, r.why))
	}
	return res, err
}

// importOne inserts one prepared import into a locked scope file. Only a new
// entry needs a free slot; a repeat stores nothing new.
func (s *Store) importOne(f *scopeFile, in Entry) (insertResult, error) {
	prep, err := s.prepare(in)
	if err != nil {
		return inserted, err
	}
	if j, _ := findMatch(f, prep); j < 0 {
		if n := pendingImports(f); n >= MaxPendingImports {
			return inserted, fmt.Errorf("%d imported entries are waiting for the developer; %w", n, ErrTooManyProposals)
		}
	}
	_, r, err := s.insert(f, in)
	return r, err
}

// pendingImports counts the pack imports in f still waiting for review
// (restores don't use up the pack limit).
func pendingImports(f *scopeFile) int {
	n := 0
	for _, x := range f.Entries {
		if pendingPack(x) {
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
