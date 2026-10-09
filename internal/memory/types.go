// Package memory gives dots persistent, inspectable, forgettable memory per
// project: shared project memory, per-dot memory and user-only global memory.
// It owns the on-disk format under STATE_DIR/memory, the file lock, dedupe,
// the secret scanner, retrieval, rendering, similarity and pruning.
package memory

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Scope string

const (
	ScopeProject Scope = "project" // shared by every dot on a project
	ScopeDot     Scope = "dot"     // one dot on one project
	ScopeGlobal  Scope = "global"  // one dot on every project; only the user writes it
)

func (s Scope) Valid() bool { return s == ScopeProject || s == ScopeDot || s == ScopeGlobal }

type Kind string

const (
	KindConvention Kind = "convention"
	KindDecision   Kind = "decision"
	KindFinding    Kind = "finding" // accepted (sent) advice
	KindDismissed  Kind = "dismissed"
	KindPreference Kind = "preference"
	KindNote       Kind = "note"
)

var Kinds = []Kind{KindConvention, KindDecision, KindFinding, KindDismissed, KindPreference, KindNote}

// ModelKinds are the only kinds a model may write.
var ModelKinds = []Kind{KindConvention, KindDecision, KindFinding, KindNote}

func (k Kind) Valid() bool { return slices.Contains(Kinds, k) }

type Status string

const (
	StatusActive   Status = "active"
	StatusProposed Status = "proposed" // awaiting approval; project scope only
	StatusArchived Status = "archived"
)

type Source string

const (
	SourceUser    Source = "user"
	SourceModel   Source = "model"
	SourceOutcome Source = "outcome"
	SourceImport  Source = "import"
)

// RememberAs is what the Dots pane's M chooser turns a card into.
type RememberAs string

const (
	AsFinding    RememberAs = "finding"    // dot scope, accepted finding
	AsRule       RememberAs = "rule"       // dot scope, "don't raise this again"
	AsConvention RememberAs = "convention" // project scope, active
	AsGlobal     RememberAs = "global"     // this dot on every project
)

func (a RememberAs) Valid() bool {
	return a == AsFinding || a == AsRule || a == AsConvention || a == AsGlobal
}

type Evidence struct {
	Session string `json:"session,omitempty"`
	Pane    string `json:"pane,omitempty"`
	Turn    int    `json:"turn,omitempty"`
	Card    string `json:"card,omitempty"`
}

// Entry is one memory item. For project-scope entries Dot is the dot that
// proposed it ("" when the developer wrote it); for dot and global scope it
// is the dot the entry belongs to.
type Entry struct {
	ID         string    `json:"id"`
	Scope      Scope     `json:"scope"`
	Dot        string    `json:"dot,omitempty"`
	Kind       Kind      `json:"kind"`
	Text       string    `json:"text"`
	Paths      []string  `json:"paths,omitempty"`
	Status     Status    `json:"status"`
	Source     Source    `json:"source"`
	Pinned     bool      `json:"pinned"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
	Uses       int       `json:"uses"`
	Evidence   Evidence  `json:"evidence"`
}

// Ref names one entry. Ids are unique within one scope file only, so a dot
// or global ref also needs the dot.
type Ref struct {
	Scope Scope
	Dot   string
	ID    string
}

func (e Entry) Ref() Ref {
	r := Ref{Scope: e.Scope, ID: e.ID}
	if e.Scope != ScopeProject {
		r.Dot = e.Dot
	}
	return r
}

// String is the display form: p:m3, d:m17, g:m2.
func (r Ref) String() string {
	switch r.Scope {
	case ScopeProject:
		return "p:" + r.ID
	case ScopeDot:
		return "d:" + r.ID
	case ScopeGlobal:
		return "g:" + r.ID
	}
	return r.ID
}

// ParseRef reads "p:m3", "d:m9" or "g:m2". dot names the dot for d: and g:.
func ParseRef(s, dot string) (Ref, error) {
	prefix, id, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || !validEntryID(id) {
		return Ref{}, fmt.Errorf("%w: memory id %q must look like p:m3, d:m9 or g:m2", ErrInvalid, s)
	}
	switch prefix {
	case "p":
		return Ref{Scope: ScopeProject, ID: id}, nil
	case "d", "g":
		if dot == "" {
			return Ref{}, fmt.Errorf("%w: %s needs a dot", ErrInvalid, s)
		}
		sc := ScopeDot
		if prefix == "g" {
			sc = ScopeGlobal
		}
		return Ref{Scope: sc, Dot: dot, ID: id}, nil
	}
	return Ref{}, fmt.Errorf("%w: memory id %q must start with p:, d: or g:", ErrInvalid, s)
}

func validEntryID(id string) bool {
	if len(id) < 2 || id[0] != 'm' {
		return false
	}
	for _, c := range id[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// GlobalID is the reserved directory for global scope.
const GlobalID = "_global"

// fileVersion is written into every memory file; readers refuse newer ones.
const fileVersion = 1

type Limits struct {
	MaxText            int // runes per entry
	MaxDot             int // live entries per dot scope file
	MaxProject         int
	MaxGlobal          int
	MaxLedger          int // ledger lines kept by Prune
	RetentionDays      int // model/outcome entries unused this long are archived
	ArchiveDays        int // archived entries are deleted after this long
	AutoApproveProject bool
	LockTimeout        time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxText: 500, MaxDot: 200, MaxProject: 300, MaxGlobal: 100, MaxLedger: 5000,
		RetentionDays: 180, ArchiveDays: 90, LockTimeout: 2 * time.Second}
}

// LimitsFromConfig applies config.toml's memory settings: project scope gets
// 1.5x and global scope 0.5x the per-dot cap.
func LimitsFromConfig(maxEntries, retentionDays int, autoApproveProject bool) Limits {
	l := DefaultLimits()
	if maxEntries > 0 {
		l.MaxDot, l.MaxProject, l.MaxGlobal = maxEntries, maxEntries*3/2, max(1, maxEntries/2)
	}
	if retentionDays > 0 {
		l.RetentionDays = retentionDays
	}
	l.AutoApproveProject = autoApproveProject
	return l
}

// lastActive is the newest of an entry's created, updated and used times.
func lastActive(e Entry) time.Time {
	t := e.CreatedAt
	if e.UpdatedAt.After(t) {
		t = e.UpdatedAt
	}
	if e.LastUsedAt.After(t) {
		t = e.LastUsedAt
	}
	return t
}

var (
	ErrNotFound = errors.New("memory entry not found")
	ErrVersion  = errors.New("memory written by a newer herdr-dots")
	ErrCorrupt  = errors.New("memory file unreadable")
	ErrLocked   = errors.New("memory is locked by another process")
	ErrTooLong  = errors.New("memory text too long")
	ErrEmpty    = errors.New("memory text is empty")
	ErrInvalid  = errors.New("invalid memory request")
	ErrDenied   = errors.New("not allowed to change this memory entry")
	ErrSecret   = errors.New("memory text looks like a secret")

	ErrTooManyProposals = errors.New("approve or reject some first")
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	return s == StatusActive || s == StatusProposed || s == StatusArchived
}

// Valid reports whether s is a known source.
func (s Source) Valid() bool {
	return s == SourceUser || s == SourceModel || s == SourceOutcome || s == SourceImport || s == SourceAgent
}
