// Package history keeps a persistent, per-dot record of every run and every
// card resolution, across Herdr sessions and projects, under
// STATE_DIR/history/<slug>.jsonl. Files are append-only JSON lines guarded by a
// per-file lock, capped at MaxLines (trimmed to TrimTo when over). History
// stays on this machine and holds only short excerpts of model text.
package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/textutil"
)

// Record types.
const (
	TypeRun     = "run"
	TypeResolve = "resolve"
)

// Run kinds (Record.Kind).
const (
	KindReview    = "review"
	KindSummon    = "summon"
	KindCouncil   = "council"
	KindAsk       = "ask"
	KindResearch  = "research"
	KindScheduled = "scheduled"
	KindCommit    = "commit"
	KindDraft     = "draft"
	KindMemory    = "memory"
)

// Card resolutions (Record.Resolution).
const (
	ResSent      = "sent"
	ResEdited    = "edited" // sent after the developer edited it
	ResDismissed = "dismissed"
	ResApplied   = "applied"   // Phase 7 patch applied to the working tree
	ResDiscarded = "discarded" // Phase 7 patch thrown away
	ResInjected  = "injected"  // delivered by inject mode, never approved
	ResOrphaned  = "orphaned"  // its pane closed while the card waited
)

// Defaults.
const (
	DefaultMaxLines  = 20000
	DefaultTrimTo    = 15000
	DefaultTrimEvery = 500
	ExcerptRunes     = 300
)

var (
	ErrInvalid = errors.New("invalid history record")
	ErrLocked  = errors.New("history is locked by another process")
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

// ValidSlug reports whether slug can name a history file.
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

// Record is one history line. Runs use T "run" and fill Kind, Outcome and the
// usage fields; resolutions use T "resolve" and fill Card and Resolution.
// Query returns runs with Resolution copied from their latest resolution.
type Record struct {
	T           string    `json:"t"`
	At          time.Time `json:"at"`
	Dot         string    `json:"dot"`
	Project     string    `json:"project,omitempty"`
	Pane        string    `json:"pane,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Turn        int       `json:"turn,omitempty"`
	Outcome     string    `json:"outcome,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	Card        string    `json:"card,omitempty"` // CardKey(session, id)
	Resolution  string    `json:"resolution,omitempty"`
	Excerpt     string    `json:"excerpt,omitempty"`
	CostUSD     float64   `json:"cost_usd,omitempty"`
	CostUnknown bool      `json:"cost_unknown,omitempty"`
	In          int       `json:"in,omitempty"`
	Out         int       `json:"out,omitempty"`
	Ms          int64     `json:"ms,omitempty"`
}

// CardKey joins a run and its resolutions. Queue card ids restart in every
// Herdr session, so the key carries the session.
func CardKey(session, id string) string {
	if id == "" {
		return ""
	}
	return session + "/" + id
}

// Store reads and writes history files under Dir. The zero limits mean the
// defaults. Safe for concurrent use; separate processes are serialized by a
// per-file lock (<slug>.lock).
type Store struct {
	Dir         string
	MaxLines    int
	TrimTo      int
	TrimEvery   int                 // appends (per process and file) between size checks
	LockTimeout time.Duration       // 0 → 2s
	Redact      func(string) string // scrubs key-shaped text from excerpts; nil = credentials.Redact
	Now         func() time.Time    // nil = time.Now

	mu      sync.Mutex
	appends map[string]int
}

func Open(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) limits() (maxLines, trimTo, every int) {
	maxLines, trimTo, every = s.MaxLines, s.TrimTo, s.TrimEvery
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if trimTo <= 0 || trimTo > maxLines {
		trimTo = min(DefaultTrimTo, maxLines)
	}
	if every <= 0 {
		every = DefaultTrimEvery
	}
	return maxLines, trimTo, every
}

func (s *Store) path(slug string) string { return filepath.Join(s.Dir, slug+".jsonl") }

// withLock runs fn holding this process's mutex and the file's lock.
func (s *Store) withLock(slug string, fn func(path string) error) error {
	if !ValidSlug(slug) {
		return fmt.Errorf("%w: dot %q", ErrInvalid, slug)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	timeout := s.LockTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	l, err := lockfile.Acquire(filepath.Join(s.Dir, slug+".lock"), timeout)
	if errors.Is(err, lockfile.ErrLocked) {
		return fmt.Errorf("%w: %s", ErrLocked, slug)
	}
	if err != nil {
		return err
	}
	defer l.Release()
	return fn(s.path(slug))
}

// Excerpt cleans model text for history: control and bidi characters
// stripped, keys redacted (credentials.Redact unless the store overrides it:
// history never holds a key), whitespace flattened, at most ExcerptRunes
// runes.
func (s *Store) excerpt(text string) string {
	text = textutil.SanitizeModelText(text)
	if s.Redact != nil {
		text = s.Redact(text)
	} else {
		text = credentials.Redact(text)
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > ExcerptRunes {
		text = string(r[:ExcerptRunes-1]) + "…"
	}
	return text
}

// Append writes one record. At defaults to now. It trims the file when a
// size check (every TrimEvery appends, and on the first append of each file in
// this process) finds it over MaxLines.
func (s *Store) Append(r Record) error {
	if r.T != TypeRun && r.T != TypeResolve {
		return fmt.Errorf("%w: type %q", ErrInvalid, r.T)
	}
	if r.At.IsZero() {
		r.At = s.now()
	}
	r.At = r.At.UTC()
	r.Excerpt = s.excerpt(r.Excerpt)
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.withLock(r.Dot, func(path string) error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		_, werr := f.Write(append(line, '\n'))
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
		if s.appends == nil {
			s.appends = map[string]int{}
		}
		s.appends[r.Dot]++
		_, _, every := s.limits()
		if n := s.appends[r.Dot]; n == 1 || n%every == 0 {
			return s.trimLocked(path)
		}
		return nil
	})
}

// Trim cuts slug's file to TrimTo lines when it is over MaxLines.
func (s *Store) Trim(slug string) error {
	return s.withLock(slug, s.trimLocked)
}

func (s *Store) trimLocked(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	maxLines, trimTo, _ := s.limits()
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) <= maxLines {
		return nil
	}
	keep := lines[len(lines)-trimTo:]
	runs := map[string]bool{}
	recs := make([]*Record, len(keep))
	for i, l := range keep {
		var r Record
		if json.Unmarshal(l, &r) != nil {
			continue // a corrupt line is dropped by the trim
		}
		recs[i] = &r
		if r.T == TypeRun && r.Card != "" {
			runs[r.Card] = true
		}
	}
	var out bytes.Buffer
	for i, l := range keep {
		r := recs[i]
		if r == nil {
			continue
		}
		if r.T == TypeResolve && !runs[r.Card] {
			continue // its run was trimmed away
		}
		out.Write(l)
		out.WriteByte('\n')
	}
	return fsutil.WriteFileAtomic(path, out.Bytes(), 0o600)
}

// read returns every parseable record of slug in file order and how many
// lines could not be parsed. A missing file is empty.
func (s *Store) read(slug string) ([]Record, int, error) {
	var out []Record
	skipped := 0
	err := s.withLock(slug, func(path string) error {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, l := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(l)) == 0 {
				continue
			}
			var r Record
			if err := json.Unmarshal(l, &r); err != nil || (r.T != TypeRun && r.T != TypeResolve) {
				skipped++
				continue
			}
			out = append(out, r)
		}
		if len(out) == 0 && skipped > 0 {
			// Nothing in the file is readable: move it aside so the dot starts
			// fresh and the damaged file stays for inspection.
			skipped = 0
			if err := os.Rename(path, path+".corrupt"); err != nil {
				_ = os.Remove(path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, skipped, nil
}

// joined returns slug's runs in file order, each with the resolution of its
// card's latest resolve record, plus the resolve records and the skip count.
// A resolution can be written before its run: an inject card is delivered
// while the run that made it is still finishing.
func (s *Store) joined(slug string) (runs, resolves []Record, skipped int, err error) {
	all, skipped, err := s.read(slug)
	if err != nil {
		return nil, nil, 0, err
	}
	byCard := map[string]int{}
	for _, r := range all {
		if r.T == TypeRun {
			r.Resolution = ""
			runs = append(runs, r)
			if r.Card != "" {
				byCard[r.Card] = len(runs) - 1
			}
		}
	}
	for _, r := range all {
		if r.T == TypeResolve {
			resolves = append(resolves, r)
			if i, ok := byCard[r.Card]; ok {
				runs[i].Resolution = r.Resolution
			}
		}
	}
	return runs, resolves, skipped, nil
}

// Query returns up to limit runs of slug at or after since, newest first,
// each with its card's resolution ("" while unresolved). limit <= 0 = all.
func (s *Store) Query(slug string, since time.Time, limit int) ([]Record, error) {
	runs, _, _, err := s.joined(slug)
	if err != nil {
		return nil, err
	}
	var out []Record
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].At.Before(since) {
			continue
		}
		out = append(out, runs[i])
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// Slugs lists the dots that have a history file.
func (s *Store) Slugs() ([]string, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if slug, ok := strings.CutSuffix(e.Name(), ".jsonl"); ok && !e.IsDir() && ValidSlug(slug) {
			out = append(out, slug)
		}
	}
	return out, nil
}
