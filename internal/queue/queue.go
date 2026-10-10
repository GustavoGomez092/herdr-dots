// Package queue stores dot messages (cards) waiting for the developer, and
// enforces how a card may move between states.
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/fsutil"
)

type Status string

const (
	StatusPending   Status = "pending"   // waiting for the developer
	StatusAuto      Status = "auto"      // will be delivered automatically when allowed
	StatusSent      Status = "sent"      // delivered (terminal)
	StatusDismissed Status = "dismissed" // thrown away (terminal)
	StatusOrphaned  Status = "orphaned"  // its main pane closed; viewable, dismissable
	StatusApplied   Status = "applied"   // a patch card applied to the working tree (terminal)
)

var (
	ErrNotFound   = errors.New("card not found")
	ErrTransition = errors.New("invalid card transition")
)

var allowed = map[Status][]Status{
	StatusPending:  {StatusSent, StatusDismissed, StatusAuto, StatusOrphaned, StatusApplied},
	StatusAuto:     {StatusSent, StatusPending, StatusDismissed, StatusOrphaned},
	StatusOrphaned: {StatusDismissed, StatusApplied}, // applied: a patch card whose pane closed mid-apply
}

type Card struct {
	ID        string    `json:"id"`
	Dot       string    `json:"dot"`
	DotName   string    `json:"dot_name"`
	Glyph     string    `json:"glyph"`
	PaneID    string    `json:"pane_id"`
	Turn      int       `json:"turn"`
	Severity  string    `json:"severity"`
	Body      string    `json:"body"`
	Mode      string    `json:"mode"`
	Approved  bool      `json:"approved"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Project   string    `json:"project,omitempty"` // memory project of the pane at creation

	// Council cards (Kind == KindCouncil) only.
	Kind     string     `json:"kind,omitempty"`     // "" = one dot's card; KindCouncil = merged council digest
	Question string     `json:"question,omitempty"` // the council question ("" = review the recent turns)
	Parts    []CardPart `json:"parts,omitempty"`    // every member's own answer, in roster order
	Note     string     `json:"note,omitempty"`     // shown under the card, never sent ("merged without chair …")

	// Patch cards (Kind == KindPatch) only. A patch card is never inject or
	// auto-delivered without approval: it is created pending in queue mode.
	PatchID    string   `json:"patch_id,omitempty"`    // the draft id: patches/<id>.patch and <id>.json
	DiffStat   string   `json:"diff_stat,omitempty"`   // "3 files +42 −7"
	PatchFlags []string `json:"patch_flags,omitempty"` // binary, mode, symlink, symlink-kept, submodule-skipped, conflicts

	// Proactive cards (Phase 8) only.
	Origin string `json:"origin,omitempty"` // "" = a turn review; else state.RunResearch, RunScheduled or RunCommit
	Label  string `json:"label,omitempty"`  // header text in place of "turn N": "commit a1b2c3d", "scheduled Mon 09:00"

	// Inbox bookkeeping (Phase 8). SourceID is the inbox ID a queue card was claimed from
	// (it makes a re-claim idempotent). ClaimedBy/ClaimedAt are inbox-only claim marks.
	SourceID  string    `json:"source_id,omitempty"`
	ClaimedBy string    `json:"claimed_by,omitempty"`
	ClaimedAt time.Time `json:"claimed_at,omitzero"`
}

// KindCouncil marks a council card: one digest merged from several dots.
const KindCouncil = "council"

// KindDigest marks a "while you were away" card: the relevant results of one
// idle period's research passes, one part per dot (Parts, like a council).
const KindDigest = "digest"

// KindPatch marks a patch card: a write-capable dot's drafted fix (Phase 7).
const KindPatch = "patch"

// FlagConflicts marks a patch card applied with conflict markers left in files.
const FlagConflicts = "conflicts"

// CardPart is one council member's answer on a council card.
type CardPart struct {
	Dot      string `json:"dot"`
	DotName  string `json:"dot_name"`
	Glyph    string `json:"glyph"`
	Severity string `json:"severity,omitempty"` // "" = the member had nothing to add
	Body     string `json:"body,omitempty"`     // the answer; for a failed member, why it failed
	Stance   string `json:"stance,omitempty"`   // round 2: "", agree, object or add
	Failed   bool   `json:"failed,omitempty"`   // timed out, errored or skipped
}

// keepTerminal bounds the file: only the most recent sent/dismissed cards stay.
const keepTerminal = 200

type Store struct {
	path  string
	mu    sync.Mutex
	cards []Card
	next  int
	tok   string // this Store's inbox claim token
}

type file struct {
	Next  int    `json:"next"`
	Cards []Card `json:"cards"`
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("queue %s: %w", path, err)
	}
	s.cards, s.next = f.Cards, f.Next
	for _, c := range s.cards { // a damaged or edited "next" never reuses an id
		if n, err := strconv.Atoi(c.ID); err == nil && n > s.next {
			s.next = n
		}
	}
	return s, nil
}

func (s *Store) save() error {
	var terminal, live []Card
	for _, c := range s.cards {
		if c.Status == StatusSent || c.Status == StatusDismissed || c.Status == StatusApplied {
			terminal = append(terminal, c)
		} else {
			live = append(live, c)
		}
	}
	if len(terminal) > keepTerminal {
		terminal = terminal[len(terminal)-keepTerminal:]
	}
	s.cards = append(live, terminal...)
	data, err := json.MarshalIndent(file{Next: s.next, Cards: s.cards}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.path, data, 0o600)
}

func (s *Store) find(id string) int {
	return slices.IndexFunc(s.cards, func(c Card) bool { return c.ID == id })
}

func (s *Store) Add(c Card) (Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.SourceID != "" {
		if i := slices.IndexFunc(s.cards, func(x Card) bool { return x.SourceID == c.SourceID }); i >= 0 {
			return s.cards[i], nil // already claimed from the inbox: idempotent
		}
	}
	c = sanitizeCard(c)
	prev, prevNext := slices.Clone(s.cards), s.next // save() reorders s.cards; keep both to roll back
	s.next++
	c.ID = strconv.Itoa(s.next)
	if c.SourceID == "" || c.CreatedAt.IsZero() { // a claimed inbox card keeps its original time
		c.CreatedAt = time.Now()
	}
	if c.Status == "" {
		c.Status = StatusPending
	}
	s.cards = append(s.cards, c)
	if err := s.save(); err != nil {
		s.cards, s.next = prev, prevNext // nothing stays in memory un-persisted
		return Card{}, err
	}
	return c, nil
}

// token is this Store's inbox claim token, one per Store (per session).
func (s *Store) token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok == "" {
		var b [8]byte
		_, _ = rand.Read(b[:])
		s.tok = hex.EncodeToString(b[:])
	}
	return s.tok
}

func (s *Store) Get(id string) (Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(id); i >= 0 {
		return s.cards[i], true
	}
	return Card{}, false
}

func rank(sev string) int {
	switch sev {
	case "critical":
		return 3
	case "warn":
		return 2
	}
	return 1
}

func (s *Store) filter(paneID string, sts ...Status) []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Card
	for _, c := range s.cards {
		if c.PaneID == paneID && slices.Contains(sts, c.Status) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i].Severity) != rank(out[j].Severity) {
			return rank(out[i].Severity) > rank(out[j].Severity)
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Store) Pending(paneID string) []Card { return s.filter(paneID, StatusPending, StatusAuto) }
func (s *Store) Visible(paneID string) []Card {
	return s.filter(paneID, StatusPending, StatusAuto, StatusOrphaned)
}

func (s *Store) All() []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Card(nil), s.cards...)
}

func (s *Store) mutate(id string, fn func(c *Card) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c := s.cards[i]
	if err := fn(&c); err != nil {
		return err
	}
	s.cards[i] = c
	return s.save()
}

func live(c *Card) error {
	if c.Status != StatusPending && c.Status != StatusAuto {
		return fmt.Errorf("%w: card %s is %s", ErrTransition, c.ID, c.Status)
	}
	return nil
}

func (s *Store) SetStatus(id string, st Status) error {
	return s.mutate(id, func(c *Card) error {
		if !slices.Contains(allowed[c.Status], st) {
			return fmt.Errorf("%w: %s → %s", ErrTransition, c.Status, st)
		}
		if st == StatusApplied && c.Kind != KindPatch {
			return fmt.Errorf("%w: only a patch card can be %s", ErrTransition, st)
		}
		c.Status = st
		if st == StatusSent || st == StatusApplied {
			c.Error = ""
		}
		return nil
	})
}

func (s *Store) Approve(id string) error {
	return s.mutate(id, func(c *Card) error {
		if err := live(c); err != nil {
			return err
		}
		c.Approved = true
		return nil
	})
}

func (s *Store) Edit(id, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("card body cannot be empty")
	}
	return s.mutate(id, func(c *Card) error {
		if err := live(c); err != nil {
			return err
		}
		c.Body = body
		return nil
	})
}

// AppendPart adds parts to a pending, unapproved digest card (KindDigest)
// with its rebuilt body and label, in one write under the store lock; the
// card's severity becomes the highest of its own and the new parts'. Any
// other card, or a digest being sent, sent, dismissed or orphaned, is left
// as it is (ErrTransition): later research then makes a new card. Input is
// sanitized like Add's, and a failed write changes nothing.
func (s *Store) AppendPart(id, body, label string, parts ...CardPart) (Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return Card{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c := s.cards[i]
	if c.Kind != KindDigest || c.Status != StatusPending || c.Approved {
		return Card{}, fmt.Errorf("%w: card %s can't take more parts", ErrTransition, id)
	}
	c.Parts = append(slices.Clone(c.Parts), parts...)
	c.Body, c.Label = body, label
	for _, p := range parts {
		if rank(p.Severity) > rank(c.Severity) {
			c.Severity = p.Severity
		}
	}
	c = sanitizeCard(c)
	prev := slices.Clone(s.cards) // save() reorders s.cards; keep it to roll back
	s.cards[i] = c
	if err := s.save(); err != nil {
		s.cards = prev
		return Card{}, err
	}
	return c, nil
}

// SetStatusNote moves a card to st like SetStatus and, in the same write,
// sets its note and adds flags (for a patch card applied with conflicts).
func (s *Store) SetStatusNote(id string, st Status, note string, flags ...string) error {
	return s.mutate(id, func(c *Card) error {
		if !slices.Contains(allowed[c.Status], st) {
			return fmt.Errorf("%w: %s → %s", ErrTransition, c.Status, st)
		}
		if st == StatusApplied && c.Kind != KindPatch {
			return fmt.Errorf("%w: only a patch card can be %s", ErrTransition, st)
		}
		c.Status, c.Note = st, note
		if st == StatusApplied {
			c.Error = ""
		}
		for _, f := range flags {
			if !slices.Contains(c.PatchFlags, f) {
				c.PatchFlags = append(c.PatchFlags, f)
			}
		}
		return nil
	})
}

// Fail returns a card to pending with an error marker after a failed delivery.
func (s *Store) Fail(id, msg string) error {
	return s.mutate(id, func(c *Card) error {
		if err := live(c); err != nil {
			return err
		}
		c.Status, c.Error = StatusPending, msg
		return nil
	})
}

func (s *Store) OrphanPane(paneID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.cards {
		c := &s.cards[i]
		if c.PaneID == paneID && (c.Status == StatusPending || c.Status == StatusAuto) {
			c.Status = StatusOrphaned
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.save()
}
