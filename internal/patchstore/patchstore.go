// Package patchstore keeps drafted patches: <id>.patch (the `git diff
// --binary` output) and <id>.json (its metadata) in one private directory,
// SessionDir/patches. The engine writes; the diff viewer only reads.
package patchstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/fsutil"
)

// Statuses (Meta.Status). A discarded patch is deleted, not kept.
const (
	StatusDrafting = "drafting" // the draft runs; one left after a crash is cleaned up at engine start
	StatusReady    = "ready"    // waiting on its patch card
	StatusApplied  = "applied"  // applied to the working tree
	StatusSent     = "sent"     // the "Patch ready" message went to the agent
	StatusRejected = "rejected" // failed validation (Reason says why); no patch file
	// StatusDropPending: resolved, but deleting its snapshot ref failed. The
	// record stays only so the next sweep retries; it goes once the ref does.
	StatusDropPending = "drop-pending"
)

var (
	ErrNotFound  = errors.New("patch not found")
	ErrInvalidID = errors.New("invalid patch id")
	ErrChanged   = errors.New("the patch file changed since it was drafted")
)

// Meta describes one draft.
type Meta struct {
	ID       string `json:"id"`
	Dot      string `json:"dot"`
	Pane     string `json:"pane"`
	Card     string `json:"card,omitempty"`
	Repo     string `json:"repo"`
	Snapshot string `json:"snapshot,omitempty"` // the snapshot commit
	Ref      string `json:"ref,omitempty"`      // its private ref
	BaseHead string `json:"base_head,omitempty"`
	Summary  string `json:"summary,omitempty"` // redacted: safe for every display reader
	// SummaryHadSecret: the summary as the dot wrote it looked like it held a
	// credential (it was redacted, or memory's scan found one). Memory's
	// outcome paths then write a redacted ledger line and no entry. The text
	// as written is never stored (older records' summary_raw is ignored).
	SummaryHadSecret bool      `json:"summary_had_secret,omitempty"`
	Stat             string    `json:"stat,omitempty"` // "3 files +42 −7"
	Files            int       `json:"files"`
	Bytes            int       `json:"bytes"`
	Paths            []string  `json:"paths,omitempty"`
	Flags            []string  `json:"flags,omitempty"`
	SHA256           string    `json:"sha256,omitempty"` // of the patch file
	Conflicts        []string  `json:"conflicts,omitempty"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Store reads and writes patches in Dir. One process (the engine) writes.
type Store struct {
	Dir string
	Now func() time.Time // nil = time.Now

	mu sync.Mutex // serializes Update's read-modify-write
}

func Open(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

var idRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidID matches worktree.ValidID: ids name files, so nothing else passes.
func ValidID(id string) bool { return idRe.MatchString(id) }

// PatchPath is where id's patch file lives (it may not exist).
func (s *Store) PatchPath(id string) string { return filepath.Join(s.Dir, id+".patch") }

func (s *Store) metaPath(id string) string { return filepath.Join(s.Dir, id+".json") }

func (s *Store) write(m *Meta) error {
	if !ValidID(m.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidID, m.ID)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	now := s.now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	data, err := json.MarshalIndent(*m, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.metaPath(m.ID), append(data, '\n'), 0o600)
}

// Put writes m's metadata.
func (s *Store) Put(m Meta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(&m)
}

// PutPatch writes the patch file, then m with Bytes and SHA256 filled in.
func (s *Store) PutPatch(m Meta, diff []byte) error {
	if !ValidID(m.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidID, m.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(s.PatchPath(m.ID), diff, 0o600); err != nil {
		return err
	}
	sum := sha256.Sum256(diff)
	m.Bytes, m.SHA256 = len(diff), hex.EncodeToString(sum[:])
	return s.write(&m)
}

// Get reads id's metadata.
func (s *Store) Get(id string) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	data, err := os.ReadFile(s.metaPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return Meta{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("patch %s metadata: %w", id, err)
	}
	if m.ID != id {
		return Meta{}, fmt.Errorf("patch %s metadata names %q", id, m.ID)
	}
	return m, nil
}

// Diff reads id's patch and metadata and checks the patch is byte for byte
// what was drafted (ErrChanged otherwise), so what the user reviews is what
// gets applied.
func (s *Store) Diff(id string) ([]byte, Meta, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, Meta{}, err
	}
	diff, err := os.ReadFile(s.PatchPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, m, fmt.Errorf("%w: %s has no patch file", ErrNotFound, id)
	}
	if err != nil {
		return nil, m, err
	}
	sum := sha256.Sum256(diff)
	if m.SHA256 == "" || hex.EncodeToString(sum[:]) != m.SHA256 {
		return nil, m, fmt.Errorf("%w: %s", ErrChanged, id)
	}
	return diff, m, nil
}

// Update applies fn to id's metadata and saves it.
func (s *Store) Update(id string, fn func(*Meta)) (Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.Get(id)
	if err != nil {
		return Meta{}, err
	}
	fn(&m)
	m.ID = id
	err = s.write(&m)
	return m, err
}

// Delete removes id's patch and metadata. Missing files are fine.
func (s *Store) Delete(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, p := range []string{s.PatchPath(id), s.metaPath(id)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// List returns every readable metadata file, oldest first, plus one error
// per unreadable one.
func (s *Store) List() ([]Meta, []error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	var out []Meta
	var errs []error
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !ValidID(id) {
			continue
		}
		m, err := s.Get(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, errs
}
