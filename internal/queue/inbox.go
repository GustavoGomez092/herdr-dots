package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/textutil"
)

// Inbox limits: cards per project and age.
const (
	inboxPerProject = 50
	inboxMaxAge     = 30 * 24 * time.Hour
)

// Inbox holds cards for projects with no attached pane (scheduled results),
// in STATE_DIR/inbox.json, shared by every Herdr session. Every access holds
// inbox.json.lock. Cards leave it only through Store.Claim, when a pane on
// their project attaches.
type Inbox struct {
	path    string
	mu      sync.Mutex
	Timeout time.Duration    // lock wait; 0 = 2s
	Now     func() time.Time // nil = time.Now
}

type inboxFile struct {
	Next  int    `json:"next"`
	Cards []Card `json:"cards"`
}

func OpenInbox(path string) *Inbox { return &Inbox{path: path} }

func (b *Inbox) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Inbox) locked(fn func(f *inboxFile) (bool, error)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	l, err := lockfile.Acquire(b.path+".lock", timeout)
	if err != nil {
		return fmt.Errorf("inbox: %w", err)
	}
	defer l.Release()
	var f inboxFile
	data, err := os.ReadFile(b.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("inbox %s: %w", b.path, err)
		}
	}
	changed, err := fn(&f)
	if err != nil || !changed {
		return err
	}
	data, err = json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(b.path, data, 0o600)
}

// Put stores c for its project (c.Project must be set). The card becomes a
// pending queue card when it is claimed; it keeps its mode, but an inbox card
// is never delivered automatically. Old cards are dropped: more than 50 per
// project, or older than 30 days.
func (b *Inbox) Put(c Card) error {
	if c.Project == "" {
		return errors.New("inbox: card has no project")
	}
	if c.Kind == KindPatch {
		return errors.New("inbox: a patch card stays in its session") // its patch file lives in the session's patch store
	}
	c = sanitizeCard(c)
	return b.locked(func(f *inboxFile) (bool, error) {
		now := b.now()
		f.Next++
		c.ID = "inbox-" + strconv.Itoa(f.Next)
		c.PaneID, c.Status, c.Approved, c.CreatedAt = "", StatusPending, false, now
		same := 0
		for _, x := range f.Cards {
			if x.Project == c.Project && now.Sub(x.CreatedAt) <= inboxMaxAge {
				same++
			}
		}
		drop := max(0, same+1-inboxPerProject) // the oldest of this project's cards go first
		var out []Card
		for _, x := range f.Cards {
			if now.Sub(x.CreatedAt) > inboxMaxAge {
				continue
			}
			if x.Project == c.Project && drop > 0 {
				drop--
				continue
			}
			out = append(out, x)
		}
		f.Cards = append(out, c)
		return true, nil
	})
}

// List returns the cards waiting for a project, oldest first.
func (b *Inbox) List(projectID string) ([]Card, error) {
	if _, err := os.Stat(b.path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []Card
	err := b.locked(func(f *inboxFile) (bool, error) {
		for _, c := range f.Cards {
			if c.Project == projectID && c.Kind != KindPatch {
				out = append(out, c)
			}
		}
		return false, nil
	})
	return out, err
}

// take removes and returns a project's cards. A patch card found in the file
// (it can only get there by a hand edit) is dropped, never returned.
func (b *Inbox) take(projectID string) ([]Card, error) {
	if _, err := os.Stat(b.path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []Card
	err := b.locked(func(f *inboxFile) (bool, error) {
		changed := false
		kept := f.Cards[:0]
		for _, c := range f.Cards {
			switch {
			case c.Kind == KindPatch && c.Project == projectID:
				changed = true
			case c.Project == projectID:
				out = append(out, c)
				changed = true
			default:
				kept = append(kept, c)
			}
		}
		f.Cards = kept
		return changed, nil
	})
	return out, err
}

// Claim moves a project's inbox cards into this queue as pending cards on
// paneID and returns how many moved. Cards that fail to be added go back to
// the inbox.
func (s *Store) Claim(in *Inbox, projectID, paneID string) (int, error) {
	if in == nil || projectID == "" {
		return 0, nil
	}
	cards, err := in.take(projectID)
	if err != nil || len(cards) == 0 {
		return 0, err
	}
	n := 0
	for i, c := range cards {
		c.PaneID, c.Status, c.Approved = paneID, StatusPending, false
		if _, err := s.Add(c); err != nil {
			var errs []error
			for _, back := range cards[i:] {
				errs = append(errs, in.Put(back))
			}
			return n, errors.Join(append([]error{err}, errs...)...)
		}
		n++
	}
	return n, nil
}

// sanitizeCard strips terminal control characters from every text field a
// model can influence, before the card is stored.
func sanitizeCard(c Card) Card {
	c.Label = textutil.SanitizeModelText(c.Label)
	c.Body = textutil.SanitizeModelText(c.Body)
	c.Note = textutil.SanitizeModelText(c.Note)
	c.Question = textutil.SanitizeModelText(c.Question)
	c.DotName = textutil.SanitizeModelText(c.DotName)
	if c.Parts != nil {
		parts := make([]CardPart, len(c.Parts))
		for i, p := range c.Parts {
			p.DotName = textutil.SanitizeModelText(p.DotName)
			p.Body = textutil.SanitizeModelText(p.Body)
			parts[i] = p
		}
		c.Parts = parts
	}
	return c
}
