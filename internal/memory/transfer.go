package memory

import (
	"fmt"

	"herdr-dots/internal/textutil"
)

// ImportResult reports what an import did. Skipped lines never contain entry text (it may be a secret).
type ImportResult struct {
	Added   int      // added or merged
	Skipped []string // one line per skipped entry or pack; it may quote a store error, so callers sanitize it before display
}

// NeedsProject reports whether any entry is non-global, so an import of es
// needs a project to store it under.
func NeedsProject(es []Entry) bool {
	for _, e := range es {
		if e.Scope != ScopeGlobal {
			return true
		}
	}
	return false
}

// ImportEntries stores parsed export entries for project pid ("" when every entry is global).
// The CLI's `memory import` and the memory screen share it. Every entry
// becomes an import with a fresh id; the caller has already shown the
// developer which entries look like secrets.
func (s *Store) ImportEntries(pid string, es []Entry) ImportResult {
	var res ImportResult
	// A proposed entry that carries "from" claims to be a dot pack's: it goes
	// through ImportPack and its limit of MaxPendingImports waiting entries
	// per scope, so a crafted backup can't bury the developer in reviews.
	packs := map[string][]Entry{}
	var packOrder []string
	for i, e := range es {
		p := ""
		if e.Scope != ScopeGlobal {
			p = pid
		}
		e.ID, e.Source = "", SourceImport
		if e.From != "" && e.Status == StatusProposed {
			// ImportPack attributes a dot-scope entry to the dot it is stored
			// under, so a restore of a dot-scope entry keeps From = that
			// dot, not a different original pack name (documented limit).
			key := e.From
			if e.Scope == ScopeDot {
				key = e.Dot
			}
			if _, ok := packs[key]; !ok {
				packOrder = append(packOrder, key)
			}
			packs[key] = append(packs[key], e)
			continue
		}
		if _, err := s.Add(p, e); err != nil {
			// The entry number only: the text may be a secret.
			res.Skipped = append(res.Skipped, fmt.Sprintf("skipped entry %d: %v", i+1, err))
			continue
		}
		res.Added++
	}
	for _, key := range packOrder {
		if pid == "" {
			res.Skipped = append(res.Skipped, fmt.Sprintf("skipped the entries of pack %s: they need a project", textutil.SanitizeModelText(key)))
			continue
		}
		r, err := s.ImportPack(pid, key, packs[key])
		res.Added += r.Added + r.Updated
		for _, why := range r.Rejected {
			res.Skipped = append(res.Skipped, fmt.Sprintf("skipped (pack %s) %s", textutil.SanitizeModelText(key), textutil.SanitizeModelText(why)))
		}
		if err != nil {
			res.Skipped = append(res.Skipped, fmt.Sprintf("skipped pack %s: %v", textutil.SanitizeModelText(key), err))
		}
	}
	return res
}
