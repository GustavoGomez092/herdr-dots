// Package state holds the engine's persisted runtime state: which dots are
// attached to which main panes, call usage, and the run outcome feed.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"sync"

	"herdr-dots/internal/fsutil"
)

var ErrNoAttachment = errors.New("no dots attached to pane")

type DotAttachment struct {
	Slug         string `json:"slug"`
	LastSeenTurn int    `json:"last_seen_turn"`
	Paused       bool   `json:"paused"`
	CooldownLeft int    `json:"cooldown_left"`
	Failures     int    `json:"failures"`

	MemorySeenTurn int `json:"memory_seen_turn"` // last turn index a memory pass covered
	MemoryPasses   int `json:"memory_passes"`    // memory passes run for this attachment

	// TurnParser is the transcript.ParserVersion LastSeenTurn counts in; a
	// file saved before the field existed reads 0 (T1). Optional, so the
	// attachments kind stays at version 1 and older engines ignore it.
	TurnParser int `json:"turn_parser,omitempty"`
}

type Attachment struct {
	PaneID          string          `json:"pane_id"`
	Agent           string          `json:"agent"`
	SessionKind     string          `json:"session_kind"`
	SessionValue    string          `json:"session_value"`
	Cwd             string          `json:"cwd"`
	Source          string          `json:"source"`
	ForceScrollback bool            `json:"force_scrollback"`
	Dots            []DotAttachment `json:"dots"`

	ProjectID   string `json:"project_id,omitempty"` // memory project (memory.Project.ID)
	ProjectName string `json:"project_name,omitempty"`
}

func (a *Attachment) Find(slug string) *DotAttachment {
	for i := range a.Dots {
		if a.Dots[i].Slug == slug {
			return &a.Dots[i]
		}
	}
	return nil
}

func (a Attachment) clone() Attachment {
	a.Dots = append([]DotAttachment(nil), a.Dots...)
	return a
}

type Attachments struct {
	path string
	mu   sync.Mutex
	m    map[string]Attachment
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

func OpenAttachments(path string) (*Attachments, error) {
	s := &Attachments{path: path, m: map[string]Attachment{}}
	if err := readJSON(path, &s.m); err != nil {
		return nil, err
	}
	if s.m == nil {
		s.m = map[string]Attachment{}
	}
	return s, nil
}

func (s *Attachments) Get(paneID string) (Attachment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[paneID]
	return a.clone(), ok
}

func (s *Attachments) List() []Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Attachment, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, a.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PaneID < out[j].PaneID })
	return out
}

func (s *Attachments) Put(a Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[a.PaneID] = a.clone()
	return writeJSON(s.path, s.m)
}

func (s *Attachments) Delete(paneID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, paneID)
	return writeJSON(s.path, s.m)
}

// Upsert applies fn to the stored attachment for base.PaneID, or to base when
// none exists, and saves, atomically. fn must not call back into the store.
func (s *Attachments) Upsert(base Attachment, fn func(*Attachment)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[base.PaneID]
	if !ok {
		a = base
	}
	a = a.clone()
	fn(&a)
	s.m[a.PaneID] = a
	return writeJSON(s.path, s.m)
}

// Update applies fn to the stored attachment and saves. fn must not call back
// into the store.
func (s *Attachments) Update(paneID string, fn func(*Attachment)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[paneID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoAttachment, paneID)
	}
	a = a.clone()
	fn(&a)
	s.m[paneID] = a
	return writeJSON(s.path, s.m)
}
