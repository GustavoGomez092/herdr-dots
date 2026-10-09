package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// PruneReport counts what Prune changed.
type PruneReport struct{ Archived, Deleted, LedgerTrimmed int }

// Prune maintains one project (spec §6.7): model- and outcome-written entries
// unused for RetentionDays are archived, scopes over their cap archive their
// lowest-value entries, archived entries older than ArchiveDays are deleted,
// and the ledger keeps its newest MaxLedger lines. Pinned and developer
// entries are never archived. Unreadable files are skipped, never rewritten.
// Global scope is developer-only and is not pruned.
func (s *Store) Prune(p string, now time.Time) (PruneReport, error) {
	var rep PruneReport
	if !ValidProjectID(p) {
		return rep, fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	dir := filepath.Join(s.Dir, p)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return rep, nil
	}
	now = now.UTC()
	retention := time.Duration(s.Limits.RetentionDays) * 24 * time.Hour
	archiveTTL := time.Duration(s.Limits.ArchiveDays) * 24 * time.Hour
	err := s.withLock(dir, func() error {
		specs := []fileSpec{{path: s.scopePath(p, ScopeProject, ""), scope: ScopeProject}}
		for _, d := range s.dotsIn(dir, "") {
			specs = append(specs, fileSpec{path: s.scopePath(p, ScopeDot, d), scope: ScopeDot, dot: d})
		}
		for _, sp := range specs {
			f, err := readScope(sp.path, sp.scope, sp.dot)
			if err != nil {
				s.logf("memory prune: skipping %s: %v", sp.path, err)
				continue
			}
			if len(f.Entries) == 0 {
				continue
			}
			changed := false
			for i := range f.Entries {
				e := &f.Entries[i]
				stale := s.Limits.RetentionDays > 0 && now.Sub(lastActive(*e)) > retention
				if e.Status != StatusArchived && !e.Pinned && (e.Source == SourceModel || e.Source == SourceOutcome) && stale {
					e.Status, e.UpdatedAt = StatusArchived, now
					rep.Archived++
					changed = true
				}
			}
			if n := s.enforceCap(&f, s.capFor(sp.scope), "", now); n > 0 {
				rep.Archived += n
				changed = true
			}
			kept := f.Entries[:0]
			for _, e := range f.Entries {
				if e.Status == StatusArchived && s.Limits.ArchiveDays > 0 && now.Sub(e.UpdatedAt) > archiveTTL {
					rep.Deleted++
					changed = true
					continue
				}
				kept = append(kept, e)
			}
			f.Entries = kept
			if changed {
				if err := writeScope(sp.path, f); err != nil {
					return err
				}
			}
		}
		n, err := s.trimLedgerLocked(p, s.Limits.MaxLedger)
		rep.LedgerTrimmed = n
		return err
	})
	return rep, err
}
