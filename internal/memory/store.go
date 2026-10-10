package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/textutil"
)

// Store reads and writes memory under Dir:
//
//	_global/dots/<slug>.json      global scope
//	<project-id>/meta.json        project id, name, roots
//	<project-id>/project.json     project scope
//	<project-id>/dots/<slug>.json dot scope
//	<project-id>/ledger.jsonl     card outcomes
//	<dir>/memory.lock             flock guarding everything in <dir>
//
// Every read and read-modify-write holds the directory's memory.lock, so
// separate processes (two engines, the manage popup, the CLI) never lose
// updates. Never call a Store method while holding an engine lock.
type Store struct {
	Dir    string
	Limits Limits
	Now    func() time.Time                 // nil = time.Now
	Logf   func(format string, args ...any) // nil = silent
}

func Open(dir string, lim Limits) *Store { return &Store{Dir: dir, Limits: lim} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

var (
	projectIDRe = regexp.MustCompile(`^[gp]-[0-9a-f]{16}$`)
	slugRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
)

// ValidProjectID reports whether p is a project id (g-/p- + 16 hex). Ids
// become directory names, so anything else is refused.
func ValidProjectID(p string) bool { return projectIDRe.MatchString(p) }

// ValidSlug reports whether slug is a dot slug safe to use as a file name.
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

func checkTarget(p string, sc Scope, dot string) error {
	if !sc.Valid() {
		return fmt.Errorf("%w: scope %q", ErrInvalid, sc)
	}
	if sc != ScopeGlobal && !ValidProjectID(p) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if sc != ScopeProject && !ValidSlug(dot) {
		return fmt.Errorf("%w: dot %q", ErrInvalid, dot)
	}
	return nil
}

func (s *Store) dirFor(p string, sc Scope) string {
	if sc == ScopeGlobal {
		return filepath.Join(s.Dir, GlobalID)
	}
	return filepath.Join(s.Dir, p)
}

func (s *Store) scopePath(p string, sc Scope, dot string) string {
	switch sc {
	case ScopeProject:
		return filepath.Join(s.Dir, p, "project.json")
	case ScopeDot:
		return filepath.Join(s.Dir, p, "dots", dot+".json")
	}
	return filepath.Join(s.Dir, GlobalID, "dots", dot+".json")
}

// ---- locking

var (
	semMu sync.Mutex
	sems  = map[string]chan struct{}{}
)

// procSem queues this process's goroutines on one lock file (channel senders
// are served in order), so flock polling only arbitrates between processes.
func procSem(path string) chan struct{} {
	semMu.Lock()
	defer semMu.Unlock()
	ch, ok := sems[path]
	if !ok {
		ch = make(chan struct{}, 1)
		sems[path] = ch
	}
	return ch
}

func (s *Store) timeout() time.Duration {
	if s.Limits.LockTimeout > 0 {
		return s.Limits.LockTimeout
	}
	return 2 * time.Second
}

// lockRetries bounds how often withLock takes the lock again after finding
// that its lock file was unlinked (by Purge) while it waited.
const lockRetries = 5

// withLock runs fn holding dir/memory.lock, waiting at most the lock timeout
// in total; then it returns ErrLocked.
//
// Purge unlinks the lock file while it holds the lock. A writer that was
// waiting on that file then holds a lock on a file no path names, which no
// one else would respect, so after taking the lock withLock checks that
// dir/memory.lock still is the locked file (os.SameFile); if not, it lets go
// and locks the current file (recreating dir: the write then starts a fresh
// project that Purge leaves alone), at most lockRetries times.
func (s *Store) withLock(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return s.lockDir(dir, true, fn)
}

// withReadLock is withLock for reads. A read never creates anything: with no
// lock file (a directory that doesn't exist yet, or one Purge is removing)
// it reads without the lock. Files are replaced atomically, so it sees each
// file whole, and there is nothing to read in a purged project.
func (s *Store) withReadLock(dir string, fn func() error) error {
	if _, err := os.Stat(filepath.Join(dir, "memory.lock")); errors.Is(err, fs.ErrNotExist) {
		return fn()
	}
	return s.lockDir(dir, false, fn)
}

// lockDir is withLock's body; create says whether a missing directory or
// lock file is made (writes) or means "read without the lock" (reads).
func (s *Store) lockDir(dir string, create bool, fn func() error) error {
	path := filepath.Join(dir, "memory.lock")
	deadline := time.Now().Add(s.timeout())
	timer := time.NewTimer(s.timeout())
	defer timer.Stop()
	sem := procSem(path)
	select {
	case sem <- struct{}{}:
	case <-timer.C:
		return fmt.Errorf("%w: %s", ErrLocked, path)
	}
	defer func() { <-sem }()
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if !create {
				if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
					return fn() // purged while we waited
				}
			} else if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
		}
		l, err := lockfile.Acquire(path, max(0, time.Until(deadline)))
		if errors.Is(err, lockfile.ErrLocked) {
			return fmt.Errorf("%w: %s", ErrLocked, path)
		}
		if errors.Is(err, fs.ErrNotExist) {
			if !create {
				return fn() // purged while we waited
			}
			if attempt < lockRetries {
				continue // Purge removed dir between the MkdirAll and the open
			}
		}
		if err != nil {
			return err
		}
		if lockAcquiredHook != nil {
			lockAcquiredHook(path)
		}
		if lockIsCurrent(l, path) {
			defer l.Release()
			return fn()
		}
		_ = l.Release()
		if attempt >= lockRetries {
			return fmt.Errorf("%w: %s (the lock file keeps being removed)", ErrLocked, path)
		}
	}
}

// lockIsCurrent reports whether path still names the file l locked.
func lockIsCurrent(l *lockfile.Lock, path string) bool {
	held, err := l.Stat()
	if err != nil {
		return false
	}
	now, err := os.Stat(path)
	return err == nil && os.SameFile(held, now)
}

// Test hooks, nil in production: lockAcquiredHook runs in withLock right
// after the file lock is taken; purgeGapHook runs in Purge after the locked
// removal, before the project directory itself goes.
var (
	lockAcquiredHook func(lockPath string)
	purgeGapHook     func()
)

// ---- scope files

type scopeFile struct {
	Version int     `json:"version"`
	Next    int     `json:"next"` // last id number handed out; ids are never reused
	Entries []Entry `json:"entries"`
}

func (f *scopeFile) find(id string) int {
	return slices.IndexFunc(f.Entries, func(e Entry) bool { return e.ID == id })
}

func idNum(id string) int {
	if !strings.HasPrefix(id, "m") {
		return 0
	}
	n, _ := strconv.Atoi(id[1:])
	return n
}

// readScope loads a scope file without ever modifying it. A missing file is
// empty. Scope and dot come from the file's location, not its contents.
func readScope(path string, sc Scope, dot string) (scopeFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return scopeFile{Version: fileVersion}, nil
	}
	if err != nil {
		return scopeFile{}, err
	}
	var f scopeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return scopeFile{}, fmt.Errorf("%w: %s: %v", ErrCorrupt, filepath.Base(path), err)
	}
	if f.Version > fileVersion {
		return scopeFile{}, fmt.Errorf("%w (%s has version %d)", ErrVersion, filepath.Base(path), f.Version)
	}
	f.Version = fileVersion
	for _, e := range f.Entries {
		f.Next = max(f.Next, idNum(e.ID))
	}
	for i := range f.Entries {
		e := &f.Entries[i]
		e.Scope = sc
		if sc != ScopeProject {
			e.Dot = dot
		}
		// Hand edits and old files get the same cleaning as new writes.
		e.Text = textutil.SanitizeModelText(e.Text)
		e.Paths = cleanPaths(e.Paths)
		if idNum(e.ID) == 0 { // hand-written entry without an id
			f.Next++
			e.ID = "m" + strconv.Itoa(f.Next)
		}
	}
	return f, nil
}

func writeScope(path string, f scopeFile) error {
	f.Version = fileVersion
	if f.Entries == nil {
		f.Entries = []Entry{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// mutate runs fn on one scope file under its directory lock and saves it when
// fn succeeds. A corrupt file is moved aside as <name>.corrupt-<time> first
// (only ever on a write); a file from a newer version is never overwritten.
func (s *Store) mutate(p string, sc Scope, dot string, fn func(f *scopeFile) error) error {
	if err := checkTarget(p, sc, dot); err != nil {
		return err
	}
	return s.withLock(s.dirFor(p, sc), func() error { return s.mutateLocked(p, sc, dot, fn) })
}

func (s *Store) mutateLocked(p string, sc Scope, dot string, fn func(f *scopeFile) error) error {
	path := s.scopePath(p, sc, dot)
	f, err := readScope(path, sc, dot)
	if errors.Is(err, ErrCorrupt) {
		aside := fmt.Sprintf("%s.corrupt-%s", path, s.now().Format("20060102T150405.000000000Z"))
		if rerr := os.Rename(path, aside); rerr != nil {
			return rerr
		}
		s.logf("memory: %v; moved it aside to %s", err, aside)
		f, err = scopeFile{Version: fileVersion}, nil
	}
	if err != nil {
		return err
	}
	if err := fn(&f); err != nil {
		return err
	}
	return writeScope(path, f)
}

// ---- entries

func (s *Store) maxText() int {
	if s.Limits.MaxText > 0 {
		return s.Limits.MaxText
	}
	return 500
}

// prepare validates and normalizes an entry before it is stored.
func (s *Store) prepare(e Entry) (Entry, error) {
	// Entry text reaches terminals and prompts: no control characters.
	e.Text = strings.TrimSpace(textutil.SanitizeModelText(e.Text))
	if e.Text == "" {
		return e, ErrEmpty
	}
	if n := utf8.RuneCountInString(e.Text); n > s.maxText() {
		return e, fmt.Errorf("%w: %d characters (max %d)", ErrTooLong, n, s.maxText())
	}
	if !e.Kind.Valid() {
		return e, fmt.Errorf("%w: kind %q", ErrInvalid, e.Kind)
	}
	if e.Status == "" {
		e.Status = StatusActive
	}
	switch e.Status {
	case StatusActive, StatusArchived:
	case StatusProposed:
		// Imported dot memory waits for approval too (Phase 9): a shared
		// "don't raise this" must not silence a dot unreviewed.
		if e.Scope != ScopeProject && !(e.Scope == ScopeDot && e.Source == SourceImport) {
			return e, fmt.Errorf("%w: only project memory (or imported dot memory) can be proposed", ErrInvalid)
		}
	default:
		return e, fmt.Errorf("%w: status %q", ErrInvalid, e.Status)
	}
	if e.Source == "" {
		e.Source = SourceUser
	}
	if e.Scope == ScopeProject && e.Dot != "" && !ValidSlug(e.Dot) {
		return e, fmt.Errorf("%w: dot %q", ErrInvalid, e.Dot)
	}
	e.Paths = cleanPaths(e.Paths)
	return e, nil
}

func cleanPaths(in []string) []string {
	var out []string
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" || len(p) > 200 || strings.IndexFunc(p, unicode.IsControl) >= 0 || !doublestar.ValidatePattern(p) || slices.Contains(out, p) {
			continue
		}
		out = append(out, p)
		if len(out) == 10 {
			break
		}
	}
	return out
}

type insertResult int

const (
	inserted insertResult = iota
	merged                // deduped into an existing entry
	yielded               // a new research entry archived at once: the scope is full of entries that outrank it
)

// insert adds e to a locked scope file. An entry of the same kind with the
// same normalized text, or a token-Jaccard >= 0.8 match, is updated instead
// (uses++). A model or outcome never rewrites the developer's wording.
func (s *Store) insert(f *scopeFile, e Entry) (Entry, insertResult, error) {
	e, err := s.prepare(e)
	if err != nil {
		return e, inserted, err
	}
	now := s.now()
	best, bestScore := findMatch(f, e)
	if best >= 0 {
		x := &f.Entries[best]
		x.Uses++
		x.UpdatedAt = now
		// The developer may reword anything. A model or outcome add may only
		// reshape an entry of its own source and dot that is not an approved
		// project convention; otherwise it just bumps uses.
		// The main agent never rewrites anything, not even its own proposal.
		// An import rewrites only an import still waiting for review, never
		// one the developer approved (Phase 9).
		rewrite := e.Source == SourceUser ||
			(e.Source != SourceAgent && x.Source != SourceUser && x.Source == e.Source && x.Dot == e.Dot && !(x.Scope == ScopeProject && x.Status != StatusProposed) &&
				!(e.Source == SourceImport && x.Status != StatusProposed))
		if rewrite {
			x.Paths = cleanPaths(append(x.Paths, e.Paths...))
			if bestScore < 1 {
				x.Text = e.Text
			}
		}
		// A pack's entry that repeats a pending import (a restore included)
		// is marked as the pack's: a pack never poses as a restore.
		if e.Source == SourceImport && e.From != "" && x.Source == SourceImport && x.Status == StatusProposed {
			x.From = e.From
		}
		if e.Source == SourceUser && x.Source != SourceUser {
			x.Source = SourceUser // the developer endorsed it
			if x.Scope == ScopeProject {
				x.Dot = ""
			}
		}
		// A sent card that repeats the dot's own model-written entry makes it
		// the card's (spec M10): only sent and user findings suppress
		// repeats, so a model must not keep a sent finding from counting by
		// writing it first. Never the reverse, and never over a user entry.
		// The same holds for an imported entry (Phase 9): the developer's own
		// verdict on a card is theirs, not someone else's shared conclusion,
		// so it keeps the card's words and none of the pack's paths:
		// suppression then matches what the developer actually saw.
		if e.Source == SourceOutcome && (x.Source == SourceModel || x.Source == SourceImport) && x.Scope == ScopeDot && x.Dot == e.Dot {
			if x.Source == SourceImport {
				x.Text, x.Paths = e.Text, nil
			}
			x.Source = SourceOutcome
			if e.Evidence != (Evidence{}) {
				x.Evidence = e.Evidence
			}
		}
		// Only the developer approves an imported proposal: a model's matching
		// add (active in dot scope, or with AutoApproveProject) leaves it
		// waiting. The developer's add and card outcome made it theirs above.
		if x.Status == StatusProposed && e.Status == StatusActive && x.Source != SourceImport {
			x.Status = StatusActive
		}
		if rewrite && e.Pinned {
			x.Pinned = true
		}
		if rewrite && e.Evidence != (Evidence{}) {
			x.Evidence = e.Evidence
		}
		return *x, merged, nil
	}
	f.Next++
	e.ID = "m" + strconv.Itoa(f.Next)
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	f.Entries = append(f.Entries, e)
	if n := s.enforceCap(f, s.capFor(e.Scope), e.ID, now); n > 0 {
		s.logf("memory: archived %d %s entries over the cap", n, e.Scope)
	}
	if last := f.Entries[len(f.Entries)-1]; last.ID == e.ID && last.Status == StatusArchived {
		e.Status, e.UpdatedAt = last.Status, last.UpdatedAt
		return e, yielded, nil
	}
	return e, inserted, nil
}

// findMatch is the live entry a prepared e dedupes into (same kind, same
// normalized text or token-Jaccard >= 0.8) and its score, or -1.
func findMatch(f *scopeFile, e Entry) (int, float64) {
	norm := Normalize(e.Text)
	best, bestScore := -1, 0.0
	for i, x := range f.Entries {
		if x.Status == StatusArchived || x.Kind != e.Kind {
			continue
		}
		if Normalize(x.Text) == norm {
			return i, 1
		}
		if sc := Similar(x.Text, e.Text); sc >= 0.8 && sc > bestScore {
			best, bestScore = i, sc
		}
	}
	return best, bestScore
}

// Cap is the entry limit of one scope.
func (s *Store) Cap(sc Scope) int { return s.capFor(sc) }

func (s *Store) capFor(sc Scope) int {
	switch sc {
	case ScopeProject:
		return s.Limits.MaxProject
	case ScopeGlobal:
		return s.Limits.MaxGlobal
	}
	return s.Limits.MaxDot
}

// enforceCap archives live entries over limit. Proposals go first, oldest
// first; active entries go (lowest value first) only while the active ones
// alone are over the limit, and never to make room for a new proposal:
// unapproved memory never pushes out approved memory. Pinned and
// developer-written entries are never archived, nor is keep.
//
// Research and question entries go before every other kind (Phase 8): fresh
// research must never push out the dismissed and finding entries that stop
// repeats, and among proposals too, so research never pushes out a convention
// the developer hasn't reviewed. When keep is itself research and no older
// research (proposed or active) can make room, keep is archived instead of any
// non-research entry. A pinned or developer-written keep is never archived.
//
// Pending pack imports (Phase 9, ruling P9-R11) sit outside the cap: they are
// never archived to make room (they leave only by approval, forgetting or
// DeleteDot), they don't count toward the limit, so local memory keeps its
// whole cap, and a new one archives nothing. MaxPendingImports bounds them
// instead, so a scope exceeds its cap by at most that many. Restored entries
// (Source import, no From) count like any other.
//
// Approved imports (Source import, active; ruling P9-R15) are someone else's
// conclusions: they go after research and before every other entry, oldest
// or lowest value first, so they never push out the developer's own outcome
// entries. When keep is itself an approved import (Update approving one into
// a full scope) and no research or other approved import can make room, keep
// is archived instead of a local entry, like research.
func (s *Store) enforceCap(f *scopeFile, limit int, keep string, now time.Time) int {
	if limit <= 0 {
		return 0
	}
	live, active, keepProposed, keepAt := 0, 0, false, -1
	keepRank := capRankNone // what keep may push out: entries of its rank or lower
	var proposed, cands []int
	for i, e := range f.Entries {
		if e.Status == StatusArchived {
			continue
		}
		if pendingPack(e) {
			if e.ID == keep {
				return 0
			}
			continue
		}
		live++
		if e.Status == StatusActive {
			active++
		}
		if e.ID == keep {
			keepProposed = e.Status == StatusProposed
			if r := capRank(e); r < capRankRest && !e.Pinned && e.Source != SourceUser {
				keepRank = r
			}
			keepAt = i
			continue
		}
		if e.Pinned || e.Source == SourceUser {
			continue
		}
		if e.Status == StatusProposed {
			proposed = append(proposed, i)
		} else {
			cands = append(cands, i)
		}
	}
	if live <= limit {
		return 0
	}
	archive := func(i int) { f.Entries[i].Status, f.Entries[i].UpdatedAt = StatusArchived, now }
	sort.SliceStable(proposed, func(a, b int) bool {
		ra, rb := capRank(f.Entries[proposed[a]]), capRank(f.Entries[proposed[b]])
		if ra != rb {
			return ra < rb
		}
		return f.Entries[proposed[a]].CreatedAt.Before(f.Entries[proposed[b]].CreatedAt)
	})
	n := 0
	for _, i := range proposed {
		if live-n <= limit {
			break
		}
		if capRank(f.Entries[i]) > keepRank {
			break // research or an import never pushes out an unreviewed local proposal
		}
		archive(i)
		n++
	}
	// yield archives the incoming research entry or approved import when
	// nothing of its rank or lower is left to make room.
	yield := func() int {
		// Never two entries for one: when the cap already archived an entry
		// of the keep's rank or lower, the fresh one stays.
		if keepRank != capRankNone && n == 0 && live-n > limit {
			archive(keepAt)
			n++
		}
		return n
	}
	over := min(live-n, active) - limit
	if keepRank != capRankNone && !keepProposed {
		// A yielding keep (research or an approved import) that proposals
		// keep over the limit: entries of its rank or lower that are older
		// go first, so the fresh one is archived only when none is left.
		over = max(over, live-n-limit)
	}
	if keepProposed || over <= 0 {
		return yield()
	}
	sort.SliceStable(cands, func(a, b int) bool {
		ra, rb := capRank(f.Entries[cands[a]]), capRank(f.Entries[cands[b]])
		if ra != rb {
			return ra < rb
		}
		return capScore(f.Entries[cands[a]], now) < capScore(f.Entries[cands[b]], now)
	})
	for _, i := range cands[:min(over, len(cands))] {
		if capRank(f.Entries[i]) > keepRank {
			break
		}
		archive(i)
		n++
	}
	return yield()
}

// Eviction ranks: lower goes first.
const (
	capRankResearch = iota // research and questions (Phase 8)
	capRankImport          // approved imports (P9-R15)
	capRankRest            // everything else
	capRankNone            // keep pushes out anything (not a yielding keep)
)

func capRank(e Entry) int {
	switch {
	case isResearchKind(e.Kind):
		return capRankResearch
	case e.Source == SourceImport && e.Status == StatusActive:
		return capRankImport
	}
	return capRankRest
}

func isResearchKind(k Kind) bool { return k == KindResearch || k == KindQuestion }

// pendingPack reports a dot pack's imported entry still waiting for the
// developer (ImportPack sets From; a restore has none).
func pendingPack(e Entry) bool {
	return e.Status == StatusProposed && e.Source == SourceImport && e.From != ""
}

// capScore ranks entries for archiving: rarely used, long unused first.
func capScore(e Entry, now time.Time) float64 {
	days := now.Sub(lastActive(e)).Hours() / 24
	return float64(min(e.Uses, 20)) + 10*math.Exp(-days/30)
}

// Add stores a new entry (with dedupe) and returns what was stored. For dot
// and global scope e.Dot names the dot; for project scope it is the author.
func (s *Store) Add(p string, e Entry) (Entry, error) {
	var out Entry
	err := s.mutate(p, e.Scope, dotFor(e.Scope, e.Dot), func(f *scopeFile) error {
		var err error
		out, _, err = s.insert(f, e)
		return err
	})
	return out, err
}

func dotFor(sc Scope, dot string) string {
	if sc == ScopeProject {
		return ""
	}
	return dot
}

func (s *Store) Get(p string, r Ref) (Entry, error) {
	if err := checkTarget(p, r.Scope, r.Dot); err != nil {
		return Entry{}, err
	}
	var out Entry
	err := s.withReadLock(s.dirFor(p, r.Scope), func() error {
		f, err := readScope(s.scopePath(p, r.Scope, r.Dot), r.Scope, r.Dot)
		if err != nil {
			return err
		}
		i := f.find(r.ID)
		if i < 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, r)
		}
		out = f.Entries[i]
		return nil
	})
	return out, err
}

// Update applies fn to one entry and saves it. fn may change text, kind,
// paths, status and pinned; id, scope and (for dot/global scope) dot are kept.
// The scope's cap is enforced as on Add (enforceCap): an approved import
// that finds the scope full is archived rather than a local entry, and the
// returned entry says so.
func (s *Store) Update(p string, r Ref, fn func(*Entry) error) (Entry, error) {
	var out Entry
	err := s.mutate(p, r.Scope, r.Dot, func(f *scopeFile) error {
		i := f.find(r.ID)
		if i < 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, r)
		}
		old := f.Entries[i]
		e := old
		if err := fn(&e); err != nil {
			return err
		}
		e.ID, e.Scope, e.CreatedAt = old.ID, old.Scope, old.CreatedAt
		if r.Scope != ScopeProject {
			e.Dot = old.Dot
		}
		e, err := s.prepare(e)
		if err != nil {
			return err
		}
		now := s.now()
		e.UpdatedAt = now
		f.Entries[i] = e
		// An approval may take the scope over its cap: enforce it here so a
		// later local add never pays for it (P9-R15).
		if n := s.enforceCap(f, s.capFor(r.Scope), e.ID, now); n > 0 {
			s.logf("memory: archived %d %s entries over the cap", n, r.Scope)
		}
		out = f.Entries[i]
		return nil
	})
	return out, err
}

// Forget deletes one entry. With scrub it also deletes the ledger lines it
// came from or that say the same thing.
func (s *Store) Forget(p string, r Ref, scrub bool) error {
	// One lock covers both steps. The ledger is scrubbed first, so a failure
	// leaves the entry in place and Forget can be retried.
	return s.mutate(p, r.Scope, r.Dot, func(f *scopeFile) error {
		i := f.find(r.ID)
		if i < 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, r)
		}
		if scrub && r.Scope != ScopeGlobal {
			if _, err := s.scrubLedgerLocked(p, f.Entries[i]); err != nil {
				return err
			}
		}
		f.Entries = slices.Delete(f.Entries, i, i+1)
		return nil
	})
}

// ForgetAll deletes every entry in one scope file and returns how many.
func (s *Store) ForgetAll(p string, sc Scope, dot string) (int, error) {
	n := 0
	err := s.mutate(p, sc, dotFor(sc, dot), func(f *scopeFile) error {
		n = len(f.Entries)
		f.Entries = nil
		return nil
	})
	return n, err
}

// Touch records that entries were put into a prompt: uses++ and last_used_at.
func (s *Store) Touch(p string, refs []Ref) error {
	byFile := map[Ref][]string{}
	for _, r := range refs {
		k := Ref{Scope: r.Scope, Dot: r.Dot}
		byFile[k] = append(byFile[k], r.ID)
	}
	now := s.now()
	var errs []error
	for k, ids := range byFile {
		errs = append(errs, s.mutate(p, k.Scope, k.Dot, func(f *scopeFile) error {
			for i := range f.Entries {
				if slices.Contains(ids, f.Entries[i].ID) {
					f.Entries[i].Uses++
					f.Entries[i].LastUsedAt = now
				}
			}
			return nil
		}))
	}
	return errors.Join(errs...)
}

// ---- listing

// Filter selects entries. Scope "" means every scope; Dot "" means every dot.
// Statuses nil means active and proposed (archived entries are hidden).
type Filter struct {
	Scope    Scope
	Dot      string
	Kinds    []Kind
	Statuses []Status
	Query    string // case-insensitive substring of text, paths or kind
}

func (f Filter) match(e Entry) bool {
	sts := f.Statuses
	if len(sts) == 0 {
		sts = []Status{StatusActive, StatusProposed}
	}
	if !slices.Contains(sts, e.Status) {
		return false
	}
	if len(f.Kinds) > 0 && !slices.Contains(f.Kinds, e.Kind) {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		hay := strings.ToLower(e.Text + " " + strings.Join(e.Paths, " ") + " " + string(e.Kind))
		if !strings.Contains(hay, q) {
			return false
		}
	}
	return true
}

// ScopeError reports one unreadable scope file; the other scopes still load.
type ScopeError struct {
	Scope Scope
	Dot   string
	Err   error
}

func (e *ScopeError) Error() string {
	name := string(e.Scope)
	if e.Dot != "" {
		name += " (" + e.Dot + ")"
	}
	return name + " memory unreadable: " + e.Err.Error()
}

func (e *ScopeError) Unwrap() error { return e.Err }

type fileSpec struct {
	dir, path string
	scope     Scope
	dot       string
}

// dotsIn lists the dot slugs with a scope file under dir/dots (or just dot).
func (s *Store) dotsIn(dir, dot string) []string {
	if dot != "" {
		return []string{dot}
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "dots"))
	var out []string
	for _, e := range ents {
		if slug, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && ValidSlug(slug) {
			out = append(out, slug)
		}
	}
	return out
}

func (s *Store) specs(p string, f Filter) ([]fileSpec, error) {
	if p != "" && !ValidProjectID(p) {
		return nil, fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if f.Scope != "" && !f.Scope.Valid() {
		return nil, fmt.Errorf("%w: scope %q", ErrInvalid, f.Scope)
	}
	if f.Dot != "" && !ValidSlug(f.Dot) {
		return nil, fmt.Errorf("%w: dot %q", ErrInvalid, f.Dot)
	}
	want := func(sc Scope) bool { return f.Scope == "" || f.Scope == sc }
	var out []fileSpec
	if p != "" && want(ScopeProject) {
		out = append(out, fileSpec{dir: s.dirFor(p, ScopeProject), path: s.scopePath(p, ScopeProject, ""), scope: ScopeProject})
	}
	if p != "" && want(ScopeDot) {
		for _, d := range s.dotsIn(s.dirFor(p, ScopeDot), f.Dot) {
			out = append(out, fileSpec{dir: s.dirFor(p, ScopeDot), path: s.scopePath(p, ScopeDot, d), scope: ScopeDot, dot: d})
		}
	}
	if want(ScopeGlobal) {
		for _, d := range s.dotsIn(s.dirFor("", ScopeGlobal), f.Dot) {
			out = append(out, fileSpec{dir: s.dirFor("", ScopeGlobal), path: s.scopePath("", ScopeGlobal, d), scope: ScopeGlobal, dot: d})
		}
	}
	return out, nil
}

// List returns matching entries sorted project → dot → global, then by id.
// p may be "" to list only global memory. Unreadable scope files are reported
// as *ScopeError values joined into the error while the rest still load.
func (s *Store) List(p string, f Filter) ([]Entry, error) {
	specs, err := s.specs(p, f)
	if err != nil {
		return nil, err
	}
	var out []Entry
	var errs []error
	for i := 0; i < len(specs); {
		j := i
		for j < len(specs) && specs[j].dir == specs[i].dir {
			j++
		}
		group := specs[i:j]
		lerr := s.withReadLock(group[0].dir, func() error {
			for _, sp := range group {
				sf, err := readScope(sp.path, sp.scope, sp.dot)
				if err != nil {
					errs = append(errs, &ScopeError{Scope: sp.scope, Dot: sp.dot, Err: err})
					continue
				}
				for _, e := range sf.Entries {
					if f.match(e) {
						out = append(out, e)
					}
				}
			}
			return nil
		})
		if lerr != nil {
			errs = append(errs, lerr)
		}
		i = j
	}
	sortEntries(out)
	return out, errors.Join(errs...)
}

func scopeRank(sc Scope) int {
	switch sc {
	case ScopeProject:
		return 0
	case ScopeDot:
		return 1
	}
	return 2
}

func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if scopeRank(a.Scope) != scopeRank(b.Scope) {
			return scopeRank(a.Scope) < scopeRank(b.Scope)
		}
		if a.Scope != ScopeProject && a.Dot != b.Dot {
			return a.Dot < b.Dot
		}
		return idNum(a.ID) < idNum(b.ID)
	})
}

// Candidates loads what a run of dot may see: its dot scope, project scope
// (active entries plus its own proposals) when readProject, and its global
// scope. Archived entries are left out, and so are imported proposals
// (Phase 9): attributed to the importing dot, they would otherwise reach its
// suppression check unreviewed.
func (s *Store) Candidates(p, dot string, readProject bool) ([]Entry, error) {
	var out []Entry
	d, derr := s.List(p, Filter{Scope: ScopeDot, Dot: dot, Statuses: []Status{StatusActive}})
	out = append(out, d...)
	var perr error
	if readProject {
		var pe []Entry
		pe, perr = s.List(p, Filter{Scope: ScopeProject})
		for _, e := range pe {
			if e.Status == StatusActive || (e.Dot == dot && e.Source != SourceImport) {
				out = append(out, e)
			}
		}
	}
	g, gerr := s.List("", Filter{Scope: ScopeGlobal, Dot: dot, Statuses: []Status{StatusActive}})
	out = append(out, g...)
	return out, errors.Join(derr, perr, gerr)
}

// Stats counts live entries: Entries[""] is project scope, Entries[slug] a
// dot's scope. Proposals counts what awaits approval: project proposals and
// the dot packs' imports pending in dot scope.
type Stats struct {
	Entries   map[string]int
	Proposals int
}

func (st Stats) Total() int {
	n := 0
	for _, v := range st.Entries {
		n += v
	}
	return n
}

func (s *Store) Stats(p string) (Stats, error) {
	st := Stats{Entries: map[string]int{}}
	es, err := s.List(p, Filter{Scope: ScopeProject})
	for _, e := range es {
		st.Entries[""]++
		if e.Status == StatusProposed {
			st.Proposals++
		}
	}
	ds, derr := s.List(p, Filter{Scope: ScopeDot})
	for _, e := range ds {
		st.Entries[e.Dot]++
		if pendingPack(e) {
			st.Proposals++
		}
	}
	return st, errors.Join(err, derr)
}

// ---- projects

type ProjectMeta struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Roots     []string  `json:"roots"`
	CreatedAt time.Time `json:"created_at"`

	DotsSeen map[string]time.Time `json:"dots_seen,omitempty"` // dot slug -> last time it was attached here (Phase 8)
}

// SeeProject records a project in meta.json: its name and every toplevel
// path it has been seen at (at most 20).
func (s *Store) SeeProject(pr Project) error {
	if !ValidProjectID(pr.ID) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, pr.ID)
	}
	dir := filepath.Join(s.Dir, pr.ID)
	return s.withLock(dir, func() error {
		path := filepath.Join(dir, "meta.json")
		var m ProjectMeta
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err == nil {
			if uerr := json.Unmarshal(data, &m); uerr != nil {
				aside := fmt.Sprintf("%s.corrupt-%s", path, s.now().Format("20060102T150405.000000000Z"))
				if rerr := os.Rename(path, aside); rerr != nil {
					return rerr
				}
				s.logf("memory: %s: %v; moved it aside to %s", path, uerr, aside)
				m = ProjectMeta{}
			}
		}
		if m.Version > fileVersion {
			return fmt.Errorf("%w (%s has version %d)", ErrVersion, path, m.Version)
		}
		changed := m.ID != pr.ID
		if changed {
			m = ProjectMeta{Version: fileVersion, ID: pr.ID, CreatedAt: s.now()}
		}
		if pr.Name != "" && m.Name != pr.Name {
			m.Name, changed = pr.Name, true
		}
		if pr.Root != "" && !slices.Contains(m.Roots, pr.Root) {
			m.Roots = append(m.Roots, pr.Root)
			if len(m.Roots) > 20 {
				m.Roots = m.Roots[len(m.Roots)-20:]
			}
			changed = true
		}
		if !changed {
			return nil
		}
		data, err = json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		return fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600)
	})
}

// Projects lists every project with memory, by name.
func (s *Store) Projects() ([]ProjectMeta, error) {
	out, perrs, err := s.projects()
	errs := []error{err}
	for _, e := range perrs {
		errs = append(errs, e)
	}
	return out, errors.Join(errs...)
}

// projects lists every project directory with its meta and the error, if any,
// from reading that project's meta (keyed by project id).
func (s *Store) projects() ([]ProjectMeta, map[string]error, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var out []ProjectMeta
	perrs := map[string]error{}
	for _, e := range ents {
		if !e.IsDir() || !ValidProjectID(e.Name()) {
			continue
		}
		m := ProjectMeta{Version: fileVersion, ID: e.Name()}
		dir := filepath.Join(s.Dir, e.Name())
		lerr := s.withReadLock(dir, func() error {
			data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("%w: %s/meta.json: %v", ErrCorrupt, e.Name(), err)
			}
			return nil
		})
		if lerr != nil {
			perrs[e.Name()] = lerr
		}
		m.ID = e.Name()
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, perrs, nil
}

// FindProject looks a project up by id, or by a path at or under one of its
// recorded roots. ok is false when nothing matches.
func (s *Store) FindProject(arg string) (ProjectMeta, bool, error) {
	ps, perrs, err := s.projects()
	if err != nil {
		return ProjectMeta{}, false, err
	}
	// A match wins over other projects' damaged metas, so one bad project
	// never blocks looking up (or purging) another, or the bad one by id.
	// Only a lock timeout on the matched project itself is an error.
	if ValidProjectID(arg) {
		for _, p := range ps {
			if p.ID == arg {
				if lerr := perrs[p.ID]; errors.Is(lerr, ErrLocked) {
					return ProjectMeta{}, false, lerr
				}
				return p, true, nil
			}
		}
	} else {
		// The deepest matching root wins: a repository nested inside another
		// one (a submodule, a vendored checkout) is its own project.
		real := realPath(arg)
		best, bestLen := -1, -1
		for i, p := range ps {
			for _, r := range p.Roots {
				if (real == r || strings.HasPrefix(real, r+string(filepath.Separator))) && len(r) > bestLen {
					best, bestLen = i, len(r)
				}
			}
		}
		if best >= 0 {
			return ps[best], true, nil
		}
	}
	// Nothing matched: say why instead of claiming there is no memory.
	var errs []error
	for _, e := range perrs {
		errs = append(errs, e)
	}
	if ValidProjectID(arg) {
		return ProjectMeta{ID: arg}, false, errors.Join(errs...)
	}
	return ProjectMeta{}, false, errors.Join(errs...)
}

// Purge deletes a project's whole memory directory. Everything in it,
// the lock file last, goes while the lock is held (lock files are opened
// with FILE_SHARE_DELETE on Windows, so the held one can be unlinked). A
// writer waiting on the old lock file notices it is gone (withLock) and
// starts a fresh project instead of writing into one being deleted. After the
// lock is released only an empty directory is removed: one a writer has
// started again keeps its acknowledged write.
func (s *Store) Purge(p string) error {
	if !ValidProjectID(p) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	dir := filepath.Join(s.Dir, p)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: project %s has no memory", ErrNotFound, p)
	}
	lock := filepath.Join(dir, "memory.lock")
	if err := s.withLock(dir, func() error {
		if err := removeAllBut(dir, "memory.lock"); err != nil {
			return err
		}
		if err := os.Remove(lock); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if purgeGapHook != nil {
		purgeGapHook()
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		ents, rerr := os.ReadDir(dir)
		if rerr != nil || len(ents) == 0 {
			return err
		}
		if len(ents) == 1 && ents[0].Name() == "memory.lock" {
			removeBareLock(dir, lock) // a reader's lock file, nothing else
		}
		return nil // otherwise a writer started the project again: keep it
	}
	return nil
}

// removeBareLock removes dir when it holds only an idle lock file, which a
// read racing Purge can leave behind. A held or replaced lock is left alone.
func removeBareLock(dir, lock string) {
	l, err := lockfile.TryLock(lock)
	if err != nil {
		return
	}
	ok := lockIsCurrent(l, lock)
	if ents, err := os.ReadDir(dir); err != nil || len(ents) != 1 {
		ok = false
	}
	if ok {
		ok = os.Remove(lock) == nil // unlinked while held, as Purge does
	}
	_ = l.Release()
	if ok {
		_ = os.Remove(dir) // fails harmlessly if a writer got in first
	}
}

// removeAllBut removes every entry of dir except keep.
func removeAllBut(dir, keep string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range ents {
		if e.Name() != keep {
			errs = append(errs, os.RemoveAll(filepath.Join(dir, e.Name())))
		}
	}
	return errors.Join(errs...)
}

// DotDeletion counts what DeleteDot removed.
type DotDeletion struct {
	Files       int // dot-scope files: one per project, plus the global one
	LedgerLines int // ledger lines left by the dot's cards
	Proposals   int // project entries the dot wrote that nobody approved
}

// String is the status line form: "3 files", "3 files, 2 ledger lines, 1 proposal".
func (d DotDeletion) String() string {
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	parts := []string{plural(d.Files, "file", "files")}
	if d.LedgerLines > 0 {
		parts = append(parts, plural(d.LedgerLines, "ledger line", "ledger lines"))
	}
	if d.Proposals > 0 {
		parts = append(parts, plural(d.Proposals, "proposal", "proposals"))
	}
	return strings.Join(parts, ", ")
}

// DeleteDot forgets a dot everywhere, so forgetting is real: its dot-scope
// memory in every project, its global memory, every ledger line its cards
// left, and the project entries it wrote that were never approved (proposed
// or archived; nobody can approve them once the dot is gone, and they would
// keep its words on disk). Approved project memory stays: the developer made
// it shared truth. Each project is handled under its own lock, ledger first
// (like Forget), and a failure in one project doesn't stop the others.
func (s *Store) DeleteDot(slug string) (DotDeletion, error) {
	var out DotDeletion
	if !ValidSlug(slug) {
		return out, fmt.Errorf("%w: dot %q", ErrInvalid, slug)
	}
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var errs []error
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || (name != GlobalID && !ValidProjectID(name)) {
			continue
		}
		dir := filepath.Join(s.Dir, name)
		errs = append(errs, s.withLock(dir, func() error {
			var perrs []error
			if name != GlobalID {
				n, err := s.scrubDotLedgerLocked(name, slug)
				out.LedgerLines += n
				perrs = append(perrs, err)
				n, err = s.dropDotProposalsLocked(name, slug)
				out.Proposals += n
				perrs = append(perrs, err)
			}
			err := os.Remove(filepath.Join(dir, "dots", slug+".json"))
			switch {
			case err == nil:
				out.Files++
			case !errors.Is(err, fs.ErrNotExist):
				perrs = append(perrs, err)
			}
			perrs = append(perrs, removeDotAsidesLocked(filepath.Join(dir, "dots"), slug))
			if name != GlobalID {
				perrs = append(perrs, s.scrubDotProactiveLocked(name, slug))
			}
			return errors.Join(perrs...)
		}))
	}
	return out, errors.Join(errs...)
}

// removeDotAsidesLocked deletes dots/<slug>.json.corrupt-* and the
// migration backups dots/<slug>.json.bak-v* in dir: they hold only that
// dot's entries, so forgetting the dot forgets them too (P7-R2). A failure is
// returned, never ignored. Mixed project-level asides and backups
// (project.json.corrupt-*, project.json.bak-v*) can't be edited safely;
// `memory purge` clears those. The caller holds the project lock.
func removeDotAsidesLocked(dir, slug string) error {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range ents {
		mine := strings.HasPrefix(e.Name(), slug+".json.corrupt-") || strings.HasPrefix(e.Name(), slug+".json.bak-v")
		if !mine || e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// scrubDotLedgerLocked deletes every ledger line of dot in project p and
// returns how many. The caller holds the project lock.
func (s *Store) scrubDotLedgerLocked(p, dot string) (int, error) {
	all, err := s.readLedgerLocked(p)
	if err != nil {
		return 0, err
	}
	kept := slices.DeleteFunc(slices.Clone(all), func(l ledgerLine) bool { return l.Dot == dot })
	n := len(all) - len(kept)
	if n == 0 {
		return 0, nil
	}
	return n, s.writeLedgerLocked(p, kept)
}

// dropDotProposalsLocked deletes the project entries dot wrote that were
// never approved (proposed or archived, not developer-written) and returns
// how many. A missing project file is left missing. The caller holds the
// project lock.
func (s *Store) dropDotProposalsLocked(p, dot string) (int, error) {
	orphan := func(e Entry) bool {
		return e.Dot == dot && e.Source != SourceUser && (e.Status == StatusProposed || e.Status == StatusArchived)
	}
	f, err := readScope(s.scopePath(p, ScopeProject, ""), ScopeProject, "")
	if err != nil {
		return 0, err
	}
	if !slices.ContainsFunc(f.Entries, orphan) {
		return 0, nil
	}
	n := 0
	err = s.mutateLocked(p, ScopeProject, "", func(f *scopeFile) error {
		before := len(f.Entries)
		f.Entries = slices.DeleteFunc(f.Entries, orphan)
		n = before - len(f.Entries)
		return nil
	})
	return n, err
}

// ---- ledger

// Outcome kinds recorded in the ledger.
const (
	OutcomeSent      = "sent"
	OutcomeEdited    = "edited" // sent after the developer edited it
	OutcomeDismissed = "dismissed"
)

// Outcome is one ledger line: what happened to a dot's card.
type Outcome struct {
	At       time.Time `json:"at"`
	Dot      string    `json:"dot"`
	Card     string    `json:"card,omitempty"`
	Kind     string    `json:"outcome"`
	Severity string    `json:"severity,omitempty"`
	Body     string    `json:"body"`
	Edited   string    `json:"edited,omitempty"`
	Turn     int       `json:"turn,omitempty"`
	Pane     string    `json:"pane,omitempty"`
	Session  string    `json:"session,omitempty"`
	// Secret: the caller knows the text held a secret that was redacted
	// before it got here (a patch summary). RecordOutcome then treats it as
	// it treats a body ScanSecrets flags. Never stored.
	Secret bool `json:"-"`
}

func (s *Store) ledgerPath(p string) string { return filepath.Join(s.Dir, p, "ledger.jsonl") }

// appendLedgerLocked appends one line; the caller holds the project lock.
func (s *Store) appendLedgerLocked(p string, o Outcome) error {
	line, err := json.Marshal(o)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.ledgerPath(p), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	return errors.Join(werr, f.Close())
}

// ledgerLine is one readable ledger line: the Outcome this version reads
// from it, and the line exactly as it was written. Rewrites (trim, scrubs)
// write the kept lines back byte for byte, so fields a newer herdr-dots added
// survive (Phase 2 VITAL: a rewrite drops only the lines it means to).
type ledgerLine struct {
	Outcome
	raw string
}

// readLedgerLocked returns the readable lines in file order. A line that
// doesn't parse (a crash's half-written tail) is left out, and so is dropped
// by the next rewrite.
func (s *Store) readLedgerLocked(p string) ([]ledgerLine, error) {
	data, err := os.ReadFile(s.ledgerPath(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ledgerLine
	for _, line := range strings.Split(string(data), "\n") {
		var o Outcome
		if line != "" && json.Unmarshal([]byte(line), &o) == nil {
			out = append(out, ledgerLine{Outcome: o, raw: line})
		}
	}
	return out, nil
}

func (s *Store) writeLedgerLocked(p string, lines []ledgerLine) error {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.raw)
		b.WriteByte('\n')
	}
	return fsutil.WriteFileAtomic(s.ledgerPath(p), []byte(b.String()), 0o600)
}

// trimLedgerLocked keeps the newest limit lines and returns how many it dropped.
func (s *Store) trimLedgerLocked(p string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	all, err := s.readLedgerLocked(p)
	if err != nil || len(all) <= limit {
		return 0, err
	}
	n := len(all) - limit
	return n, s.writeLedgerLocked(p, all[n:])
}

// Ledger returns a project's ledger, oldest first.
func (s *Store) Ledger(p string) ([]Outcome, error) {
	if !ValidProjectID(p) {
		return nil, fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	var out []Outcome
	err := s.withReadLock(filepath.Join(s.Dir, p), func() error {
		lines, err := s.readLedgerLocked(p)
		for _, l := range lines {
			out = append(out, l.Outcome)
		}
		return err
	})
	return out, err
}

// scrubLedgerLocked deletes the ledger lines of e's dot that came from e's
// card or say the same thing as e. The caller holds the project lock.
func (s *Store) scrubLedgerLocked(p string, e Entry) (int, error) {
	n := 0
	err := func() error {
		all, err := s.readLedgerLocked(p)
		if err != nil {
			return err
		}
		// An "Applied patch: …" finding merged every card that applied the
		// same patch; compare those cards' bodies with the text after the
		// prefix so the earlier cards' lines go too (M17).
		text := e.Text
		if t, ok := strings.CutPrefix(e.Text, appliedPrefix); ok && e.Scope == ScopeDot {
			text = t
		}
		says := func(body string) bool {
			return body != "" && (SuppressScore(body, e.Text) >= 0.8 || SuppressScore(body, text) >= 0.8)
		}
		kept := all[:0]
		for _, o := range all {
			sameDot := e.Scope == ScopeProject || o.Dot == e.Dot
			// Card ids restart in every Herdr session: a card id names the
			// entry's own card only in its session, and only on its dot's
			// lines when a dot proposed the entry (M17).
			ownCard := e.Evidence.Card != "" && o.Card == e.Evidence.Card && o.Session == e.Evidence.Session &&
				(e.Dot == "" || o.Dot == e.Dot)
			hit := sameDot && (ownCard || says(o.Body) || says(o.Edited))
			if hit {
				n++
				continue
			}
			kept = append(kept, o)
		}
		if n == 0 {
			return nil
		}
		return s.writeLedgerLocked(p, kept)
	}()
	return n, err
}
