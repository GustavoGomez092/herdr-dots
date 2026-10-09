// Package queue stores dot messages (cards) waiting for the developer, and
// enforces how a card may move between states.
package queue

import (
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
)

var (
	ErrNotFound   = errors.New("card not found")
	ErrTransition = errors.New("invalid card transition")
)

var allowed = map[Status][]Status{
	StatusPending:  {StatusSent, StatusDismissed, StatusAuto, StatusOrphaned},
	StatusAuto:     {StatusSent, StatusPending, StatusDismissed, StatusOrphaned},
	StatusOrphaned: {StatusDismissed},
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
}

// keepTerminal bounds the file: only the most recent sent/dismissed cards stay.
const keepTerminal = 200

type Store struct {
	path  string
	mu    sync.Mutex
	cards []Card
	next  int
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
	return s, nil
}

func (s *Store) save() error {
	var terminal, live []Card
	for _, c := range s.cards {
		if c.Status == StatusSent || c.Status == StatusDismissed {
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
	s.next++
	c.ID = strconv.Itoa(s.next)
	c.CreatedAt = time.Now()
	if c.Status == "" {
		c.Status = StatusPending
	}
	s.cards = append(s.cards, c)
	return c, s.save()
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
		c.Status = st
		if st == StatusSent {
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
