package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/textutil"
)

// Inbox limits: cards per project and age, and how long a claim mark holds.
const (
	inboxPerProject = 50
	inboxMaxAge     = 30 * 24 * time.Hour
	claimTTL        = 5 * time.Minute
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

// exists is the cheap fast path of the readers: with no inbox file there is
// nothing to read, so they skip creating the directory and the lock file. A
// racing first Put is simply seen by the next call.
func (b *Inbox) exists() bool {
	_, err := os.Stat(b.path)
	return !errors.Is(err, fs.ErrNotExist)
}

// locked runs fn on the inbox file under inbox.json.lock and writes the file
// back (atomically, 0600) when fn reports a change. With recoverCorrupt (Put
// only) an unreadable file is moved aside as inbox.json.corrupt-<ts> and the
// call carries on with an empty inbox, so a new paid result is never dropped.
func (b *Inbox) locked(recoverCorrupt bool, fn func(f *inboxFile) (bool, error)) error {
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
			if !recoverCorrupt {
				return fmt.Errorf("inbox %s: %w", b.path, err)
			}
			aside := b.path + ".corrupt-" + b.now().UTC().Format("20060102T150405.000Z")
			if rerr := os.Rename(b.path, aside); rerr != nil {
				return fmt.Errorf("inbox %s: %w (and could not move it aside: %v)", b.path, err, rerr)
			}
			log.Printf("inbox: %s was unreadable (%v); moved to %s", b.path, err, aside)
			f = inboxFile{}
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

// validOrigin lists what a card's Origin may be: a turn review, or one of
// the proactive run kinds.
func validOrigin(o string) bool {
	switch o {
	case "", "research", "scheduled", "commit":
		return true
	}
	return false
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
	if !validOrigin(c.Origin) {
		return fmt.Errorf("inbox: unknown origin %q", c.Origin)
	}
	c = sanitizeCard(c)
	return b.locked(true, func(f *inboxFile) (bool, error) {
		now := b.now()
		f.Next++
		c.ID = "inbox-" + strconv.Itoa(f.Next)
		c.PaneID, c.Status, c.Approved, c.CreatedAt = "", StatusPending, false, now
		c.SourceID, c.ClaimedBy, c.ClaimedAt = "", "", time.Time{}
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

// List returns the cards waiting for a project, oldest first (claimed-but-not-
// yet-deleted ones included).
func (b *Inbox) List(projectID string) ([]Card, error) {
	if !b.exists() {
		return nil, nil
	}
	var out []Card
	err := b.locked(false, func(f *inboxFile) (bool, error) {
		for _, c := range f.Cards {
			if c.Project == projectID && c.Kind != KindPatch {
				out = append(out, c)
			}
		}
		return false, nil
	})
	return out, err
}

// mark is phase (a) of a claim: under the lock it stamps ClaimedBy/ClaimedAt on
// the project's cards and returns them, without deleting anything. Cards
// whose mark is still younger than claimTTL belong to another claimer and are
// skipped; an older mark (a claimer that died) is taken over. A patch card
// found in the file (only a hand edit can put one there) is dropped, never
// returned.
func (b *Inbox) mark(projectID, token string) ([]Card, error) {
	if !b.exists() {
		return nil, nil
	}
	var out []Card
	err := b.locked(false, func(f *inboxFile) (bool, error) {
		now := b.now()
		changed := false
		kept := f.Cards[:0]
		for _, c := range f.Cards {
			if c.Project != projectID {
				kept = append(kept, c)
				continue
			}
			if c.Kind == KindPatch {
				changed = true
				continue
			}
			if c.ClaimedBy != "" && now.Sub(c.ClaimedAt) <= claimTTL {
				kept = append(kept, c)
				continue
			}
			c.ClaimedBy, c.ClaimedAt = token, now
			kept = append(kept, c)
			out = append(out, c)
			changed = true
		}
		f.Cards = kept
		return changed, nil
	})
	return out, err
}

// remove is phase (c): under the lock it deletes exactly the cards with these
// IDs. With release, it instead clears token's marks on them and keeps them.
func (b *Inbox) remove(ids []string, token string, release bool) error {
	if len(ids) == 0 {
		return nil
	}
	return b.locked(false, func(f *inboxFile) (bool, error) {
		changed := false
		kept := f.Cards[:0]
		for _, c := range f.Cards {
			if !slices.Contains(ids, c.ID) {
				kept = append(kept, c)
				continue
			}
			changed = true
			if release {
				if c.ClaimedBy == token {
					c.ClaimedBy, c.ClaimedAt = "", time.Time{}
				}
				kept = append(kept, c)
			}
		}
		f.Cards = kept
		return changed, nil
	})
}

// Claim moves a project's inbox cards into this queue as pending, unapproved
// cards on paneID and returns how many it added. It never loses or duplicates
// a card, in three phases: (a) mark the cards under the inbox lock without
// deleting them; (b) add each to the queue with SourceID = its inbox ID,
// skipping any already there (so a re-claim after a crash is idempotent), in
// the original order and with the original CreatedAt; (c) delete exactly the
// claimed IDs. A claimer that dies leaves marks that expire after claimTTL;
// if an add fails, the cards not yet added are unmarked and stay in the inbox.
func (s *Store) Claim(in *Inbox, projectID, paneID string) (int, error) {
	if in == nil || projectID == "" {
		return 0, nil
	}
	tok := s.token()
	cards, err := in.mark(projectID, tok)
	if err != nil || len(cards) == 0 {
		return 0, err
	}
	n := 0
	var done, rest []string
	var addErr error
	for i, c := range cards {
		inboxID := c.ID
		if _, have := s.bySource(inboxID); have {
			done = append(done, inboxID) // added by an earlier, interrupted claim
			continue
		}
		c.PaneID, c.Status, c.Approved = paneID, StatusPending, false
		c.SourceID, c.ClaimedBy, c.ClaimedAt = inboxID, "", time.Time{}
		if _, err := s.Add(c); err != nil {
			addErr = err
			for _, r := range cards[i:] {
				rest = append(rest, r.ID)
			}
			break
		}
		done = append(done, inboxID)
		n++
	}
	errs := []error{addErr}
	errs = append(errs, in.remove(done, tok, false))
	errs = append(errs, in.remove(rest, tok, true))
	return n, errors.Join(errs...)
}

func (s *Store) bySource(id string) (Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.IndexFunc(s.cards, func(c Card) bool { return c.SourceID == id }); i >= 0 {
		return s.cards[i], true
	}
	return Card{}, false
}

// sanitizeCard strips terminal control characters from every text field a
// model can influence, before the card is stored.
func sanitizeCard(c Card) Card {
	c.Dot = textutil.SanitizeModelText(c.Dot)
	c.DotName = textutil.SanitizeModelText(c.DotName)
	c.Glyph = textutil.SanitizeModelText(c.Glyph)
	c.Label = textutil.SanitizeModelText(c.Label)
	c.Body = textutil.SanitizeModelText(c.Body)
	c.Note = textutil.SanitizeModelText(c.Note)
	c.Question = textutil.SanitizeModelText(c.Question)
	if c.Parts != nil {
		parts := make([]CardPart, len(c.Parts))
		for i, p := range c.Parts {
			p.Dot = textutil.SanitizeModelText(p.Dot)
			p.DotName = textutil.SanitizeModelText(p.DotName)
			p.Glyph = textutil.SanitizeModelText(p.Glyph)
			p.Stance = textutil.SanitizeModelText(p.Stance)
			p.Body = textutil.SanitizeModelText(p.Body)
			parts[i] = p
		}
		c.Parts = parts
	}
	return c
}
