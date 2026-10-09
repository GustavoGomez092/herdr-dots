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

// withLock runs fn holding dir/memory.lock, waiting at most the lock timeout
// in total; then it returns ErrLocked.
func (s *Store) withLock(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
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
	l, err := lockfile.Acquire(path, max(0, time.Until(deadline)))
	if errors.Is(err, lockfile.ErrLocked) {
		return fmt.Errorf("%w: %s", ErrLocked, path)
	}
	if err != nil {
		return err
	}
	defer l.Release()
	return fn()
}

// withReadLock is withLock for reads; a directory that doesn't exist yet is
// read without creating it.
func (s *Store) withReadLock(dir string, fn func() error) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fn()
	}
	return s.withLock(dir, fn)
}

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
		if e.Scope != ScopeProject {
			return e, fmt.Errorf("%w: only project memory can be proposed", ErrInvalid)
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
	norm := Normalize(e.Text)
	best, bestScore := -1, 0.0
	for i, x := range f.Entries {
		if x.Status == StatusArchived || x.Kind != e.Kind {
			continue
		}
		if Normalize(x.Text) == norm {
			best, bestScore = i, 1
			break
		}
		if sc := Similar(x.Text, e.Text); sc >= 0.8 && sc > bestScore {
			best, bestScore = i, sc
		}
	}
	if best >= 0 {
		x := &f.Entries[best]
		x.Uses++
		x.UpdatedAt = now
		x.Paths = cleanPaths(append(x.Paths, e.Paths...))
		if bestScore < 1 && (x.Source != SourceUser || e.Source == SourceUser) {
			x.Text = e.Text
		}
		if e.Source == SourceUser && x.Source != SourceUser {
			x.Source = SourceUser // the developer endorsed it
			if x.Scope == ScopeProject {
				x.Dot = ""
			}
		}
		if x.Status == StatusProposed && e.Status == StatusActive {
			x.Status = StatusActive
		}
		if e.Pinned {
			x.Pinned = true
		}
		if e.Evidence != (Evidence{}) {
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
	return e, inserted, nil
}

func (s *Store) capFor(sc Scope) int {
	switch sc {
	case ScopeProject:
		return s.Limits.MaxProject
	case ScopeGlobal:
		return s.Limits.MaxGlobal
	}
	return s.Limits.MaxDot
}

// enforceCap archives the lowest-value live entries over limit. Pinned and
// developer-written entries are never archived, nor is keep.
func (s *Store) enforceCap(f *scopeFile, limit int, keep string, now time.Time) int {
	if limit <= 0 {
		return 0
	}
	live := 0
	var cands []int
	for i, e := range f.Entries {
		if e.Status == StatusArchived {
			continue
		}
		live++
		if !e.Pinned && e.Source != SourceUser && e.ID != keep {
			cands = append(cands, i)
		}
	}
	over := live - limit
	if over <= 0 {
		return 0
	}
	sort.SliceStable(cands, func(a, b int) bool {
		return capScore(f.Entries[cands[a]], now) < capScore(f.Entries[cands[b]], now)
	})
	n := 0
	for _, i := range cands {
		if n == over {
			break
		}
		f.Entries[i].Status, f.Entries[i].UpdatedAt = StatusArchived, now
		n++
	}
	return n
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
		e.UpdatedAt = s.now()
		f.Entries[i] = e
		out = e
		return nil
	})
	return out, err
}

// Forget deletes one entry. With scrub it also deletes the ledger lines it
// came from or that say the same thing.
func (s *Store) Forget(p string, r Ref, scrub bool) error {
	var gone Entry
	err := s.mutate(p, r.Scope, r.Dot, func(f *scopeFile) error {
		i := f.find(r.ID)
		if i < 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, r)
		}
		gone = f.Entries[i]
		f.Entries = slices.Delete(f.Entries, i, i+1)
		return nil
	})
	if err != nil || !scrub || r.Scope == ScopeGlobal {
		return err
	}
	_, err = s.scrubLedger(p, gone)
	return err
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
// scope. Archived entries are left out.
func (s *Store) Candidates(p, dot string, readProject bool) ([]Entry, error) {
	var out []Entry
	d, derr := s.List(p, Filter{Scope: ScopeDot, Dot: dot, Statuses: []Status{StatusActive}})
	out = append(out, d...)
	var perr error
	if readProject {
		var pe []Entry
		pe, perr = s.List(p, Filter{Scope: ScopeProject})
		for _, e := range pe {
			if e.Status == StatusActive || e.Dot == dot {
				out = append(out, e)
			}
		}
	}
	g, gerr := s.List("", Filter{Scope: ScopeGlobal, Dot: dot, Statuses: []Status{StatusActive}})
	out = append(out, g...)
	return out, errors.Join(derr, perr, gerr)
}

// Stats counts live entries: Entries[""] is project scope, Entries[slug] a
// dot's scope. Proposals counts project entries awaiting approval.
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
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &m)
		}
		if m.Version > fileVersion {
			return ErrVersion
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
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		return fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600)
	})
}

// Projects lists every project with memory, by name.
func (s *Store) Projects() ([]ProjectMeta, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ProjectMeta
	for _, e := range ents {
		if !e.IsDir() || !ValidProjectID(e.Name()) {
			continue
		}
		m := ProjectMeta{Version: fileVersion, ID: e.Name()}
		dir := filepath.Join(s.Dir, e.Name())
		_ = s.withReadLock(dir, func() error {
			if data, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
				_ = json.Unmarshal(data, &m)
			}
			return nil
		})
		m.ID = e.Name()
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// FindProject looks a project up by id, or by a path at or under one of its
// recorded roots. ok is false when nothing matches.
func (s *Store) FindProject(arg string) (ProjectMeta, bool, error) {
	ps, err := s.Projects()
	if err != nil {
		return ProjectMeta{}, false, err
	}
	if ValidProjectID(arg) {
		for _, p := range ps {
			if p.ID == arg {
				return p, true, nil
			}
		}
		return ProjectMeta{ID: arg}, false, nil
	}
	real := realPath(arg)
	for _, p := range ps {
		for _, r := range p.Roots {
			if real == r || strings.HasPrefix(real, r+string(filepath.Separator)) {
				return p, true, nil
			}
		}
	}
	return ProjectMeta{}, false, nil
}

// Purge deletes a project's whole memory directory.
func (s *Store) Purge(p string) error {
	if !ValidProjectID(p) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	dir := filepath.Join(s.Dir, p)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: project %s has no memory", ErrNotFound, p)
	}
	return s.withLock(dir, func() error { return os.RemoveAll(dir) })
}

// DeleteDot removes a dot's memory in every project and its global memory,
// returning how many files were removed.
func (s *Store) DeleteDot(slug string) (int, error) {
	if !ValidSlug(slug) {
		return 0, fmt.Errorf("%w: dot %q", ErrInvalid, slug)
	}
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, e := range ents {
		if !e.IsDir() || (e.Name() != GlobalID && !ValidProjectID(e.Name())) {
			continue
		}
		dir := filepath.Join(s.Dir, e.Name())
		path := filepath.Join(dir, "dots", slug+".json")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		errs = append(errs, s.withLock(dir, func() error {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			n++
			return nil
		}))
	}
	return n, errors.Join(errs...)
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

func (s *Store) readLedgerLocked(p string) ([]Outcome, error) {
	data, err := os.ReadFile(s.ledgerPath(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Outcome
	for _, line := range strings.Split(string(data), "\n") {
		var o Outcome
		if line != "" && json.Unmarshal([]byte(line), &o) == nil {
			out = append(out, o)
		}
	}
	return out, nil
}

func (s *Store) writeLedgerLocked(p string, outs []Outcome) error {
	var b strings.Builder
	for _, o := range outs {
		line, err := json.Marshal(o)
		if err != nil {
			return err
		}
		b.Write(line)
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
		var err error
		out, err = s.readLedgerLocked(p)
		return err
	})
	return out, err
}

// scrubLedger deletes the ledger lines of e's dot that came from e's card or
// say the same thing as e.
func (s *Store) scrubLedger(p string, e Entry) (int, error) {
	n := 0
	err := s.withLock(filepath.Join(s.Dir, p), func() error {
		all, err := s.readLedgerLocked(p)
		if err != nil {
			return err
		}
		kept := all[:0]
		for _, o := range all {
			sameDot := e.Scope == ScopeProject || o.Dot == e.Dot
			hit := sameDot && ((e.Evidence.Card != "" && o.Card == e.Evidence.Card) ||
				SuppressScore(o.Body, e.Text) >= 0.8 || (o.Edited != "" && SuppressScore(o.Edited, e.Text) >= 0.8))
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
	})
	return n, err
}
