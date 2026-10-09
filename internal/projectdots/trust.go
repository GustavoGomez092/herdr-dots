// Package projectdots finds the team dots a repository ships in
// .herdr-dots/dots/ and gates them behind a direnv-style trust store: trust is
// per file content (a SHA-256 of the exact bytes), so any change makes a file
// untrusted again. Untrusted files can't be attached; trusted files run with
// every risky capability clamped except the ones the developer granted.
package projectdots

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
	"slices"
	"sort"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
)

// trustVersion is written into trusted.json; newer files are refused.
const trustVersion = 1

// projectIDRe is a memory project id (memory.ResolveProject, ruling 11): g-
// and the first 16 hex digits of the root commit, or p- and 16 of a path hash.
var projectIDRe = regexp.MustCompile(`^[gp]-[0-9a-f]{16}$`)

var (
	ErrCorrupt = errors.New("trusted.json is unreadable: fix or delete it (every project dot stays untrusted until then)")
	ErrLocked  = errors.New("trusted.json is locked by another process")
	ErrNewer   = errors.New("trusted.json was written by a newer herdr-dots: upgrade (every project dot stays untrusted until then)")
)

// TrustEntry is one trusted file. The key is (Project, Rel, SHA256): the
// same bytes at the same place in any clone or worktree of the same
// repository (decision #11: project identity is the git root commit).
type TrustEntry struct {
	Project      string    `json:"project"`
	Rel          string    `json:"rel"`
	Path         string    `json:"path"` // where it was trusted (display only)
	SHA256       string    `json:"sha256"`
	Capabilities []string  `json:"capabilities"`
	TrustedAt    time.Time `json:"trusted_at"`
}

type trustDoc struct {
	Version int          `json:"version"`
	Entries []TrustEntry `json:"entries"`
}

// TrustStore is CONFIG_DIR/trusted.json.
type TrustStore struct {
	Path        string
	LockTimeout time.Duration    // 0 → 2s
	Now         func() time.Time // nil = time.Now
}

func Hex(sum [32]byte) string { return hex.EncodeToString(sum[:]) }

// Sum is the trust hash of a file's bytes.
func Sum(data []byte) [32]byte { return sha256.Sum256(data) }

// plainErr drops the path from a file system error, so no absolute path (a
// home directory) reaches the UI; callers name the file themselves.
func plainErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return err
}

// load reads the trust file and returns its raw bytes with the parsed doc.
// A missing file is an empty store. A file that doesn't parse is ErrCorrupt
// and one a newer version wrote is ErrNewer; either way nothing is trusted.
func (t *TrustStore) load() (trustDoc, []byte, error) {
	data, err := os.ReadFile(t.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return trustDoc{Version: trustVersion}, nil, nil
	}
	if err != nil {
		return trustDoc{}, nil, fmt.Errorf("trusted.json: %w", plainErr(err))
	}
	var doc trustDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version < 1 {
		return trustDoc{}, data, ErrCorrupt
	}
	if doc.Version > trustVersion {
		return trustDoc{}, data, ErrNewer
	}
	return doc, data, nil
}

func (t *TrustStore) read() (trustDoc, error) {
	doc, _, err := t.load()
	return doc, err
}

// snapshot is one read of the store: the doc a scan checks every file
// against, and the stamp of the bytes it came from ("none" without a file).
func (t *TrustStore) snapshot() (trustDoc, string, error) {
	doc, raw, err := t.load()
	stamp := "none"
	if raw != nil {
		stamp = Hex(Sum(raw))
	}
	return doc, stamp, err
}

// find reports whether this exact content of project/rel is trusted, and
// the capabilities granted with it.
func (d trustDoc) find(project, rel string, sum [32]byte) (bool, []string) {
	e, ok := d.entry(project, rel, sum)
	return ok, e.Capabilities
}

// entry is the trust entry for this exact content of project/rel, with its
// capabilities copied.
func (d trustDoc) entry(project, rel string, sum [32]byte) (TrustEntry, bool) {
	h := Hex(sum)
	for _, e := range d.Entries {
		if e.Project == project && e.Rel == rel && e.SHA256 == h {
			e.Capabilities = slices.Clone(e.Capabilities)
			return e, true
		}
	}
	return TrustEntry{}, false
}

// List returns every trust entry, newest first.
func (t *TrustStore) List() ([]TrustEntry, error) {
	doc, err := t.read()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(doc.Entries, func(i, j int) bool { return doc.Entries[i].TrustedAt.After(doc.Entries[j].TrustedAt) })
	return doc.Entries, nil
}

// Check reports whether this exact content is trusted and which capabilities
// were granted. Any error means untrusted.
func (t *TrustStore) Check(project, rel string, sum [32]byte) (bool, []string, error) {
	doc, err := t.read()
	if err != nil {
		return false, nil, err
	}
	ok, caps := doc.find(project, rel, sum)
	return ok, caps, nil
}

// mutate applies fn to the store under its lock and replaces the file
// atomically (0600). A corrupt file is never overwritten: a copy is kept
// aside as trusted.json.corrupt and the change fails with ErrCorrupt. A
// newer file is left alone (ErrNewer).
func (t *TrustStore) mutate(fn func(*trustDoc)) error {
	timeout := t.LockTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return fmt.Errorf("trusted.json: %w", plainErr(err))
	}
	l, err := lockfile.Acquire(t.Path+".lock", timeout)
	if errors.Is(err, lockfile.ErrLocked) {
		return ErrLocked
	}
	if err != nil {
		return fmt.Errorf("trusted.json: %w", plainErr(err))
	}
	defer l.Release()
	doc, raw, err := t.load()
	if errors.Is(err, ErrCorrupt) {
		if aerr := fsutil.WriteFileAtomic(t.Path+".corrupt", raw, 0o600); aerr != nil {
			return fmt.Errorf("%w (and keeping a copy failed: %v)", ErrCorrupt, plainErr(aerr))
		}
		return err
	}
	if err != nil {
		return err // never overwrite a file we couldn't read
	}
	fn(&doc)
	doc.Version = trustVersion
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(t.Path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("trusted.json: %w", plainErr(err))
	}
	return nil
}

// MaxTrustPerFile caps the entries one file (project and rel) keeps: one per
// checkout that trusted it, the newest kept.
const MaxTrustPerFile = 16

// Trust records this content of project/rel as trusted with caps (which must
// be known capabilities). It replaces the entry for the same bytes and the
// earlier entry given at the same path (this checkout's older content); other
// checkouts' entries for other content stay (ruling P9-R13: each checkout of
// a repository trusts its own files, and identical bytes trusted in one are
// trusted in all). project must be a memory project id (ruling 11), so trust
// given in one repository never applies to another.
func (t *TrustStore) Trust(project, rel, path string, sum [32]byte, caps []string) error {
	if !projectIDRe.MatchString(project) || rel == "" {
		return errors.New("trust needs a project id (g- or p- and 16 hex digits) and a file")
	}
	if err := dots.CheckCapabilities(caps); err != nil {
		return err
	}
	now := time.Now()
	if t.Now != nil {
		now = t.Now()
	}
	caps = slices.Clone(caps)
	if caps == nil {
		caps = []string{}
	}
	h := Hex(sum)
	return t.mutate(func(d *trustDoc) {
		d.Entries = slices.DeleteFunc(d.Entries, func(e TrustEntry) bool {
			return e.Project == project && e.Rel == rel && (e.SHA256 == h || e.Path == path)
		})
		d.Entries = append(d.Entries, TrustEntry{Project: project, Rel: rel, Path: path, SHA256: h, Capabilities: caps, TrustedAt: now.UTC()})
		var same []int
		for i, e := range d.Entries {
			if e.Project == project && e.Rel == rel {
				same = append(same, i)
			}
		}
		if extra := len(same) - MaxTrustPerFile; extra > 0 {
			sort.SliceStable(same, func(i, j int) bool { return d.Entries[same[i]].TrustedAt.Before(d.Entries[same[j]].TrustedAt) })
			drop := map[int]bool{}
			for _, i := range same[:extra] {
				drop[i] = true
			}
			kept := d.Entries[:0]
			for i, e := range d.Entries {
				if !drop[i] {
					kept = append(kept, e)
				}
			}
			d.Entries = kept
		}
	})
}

// RevokeSum forgets the trust of this content of project/rel (every
// checkout's), and reports whether there was one.
func (t *TrustStore) RevokeSum(project, rel string, sum [32]byte) (bool, error) {
	h := Hex(sum)
	return t.revoke(func(e TrustEntry) bool { return e.Project == project && e.Rel == rel && e.SHA256 == h })
}

// RevokePath forgets the trust given at path for project/rel (one checkout's
// entry, whatever its content: the file may be gone or broken).
func (t *TrustStore) RevokePath(project, rel, path string) (bool, error) {
	return t.revoke(func(e TrustEntry) bool { return e.Project == project && e.Rel == rel && e.Path == path })
}

func (t *TrustStore) revoke(match func(TrustEntry) bool) (bool, error) {
	found := false
	err := t.mutate(func(d *trustDoc) {
		n := len(d.Entries)
		d.Entries = slices.DeleteFunc(d.Entries, match)
		found = len(d.Entries) != n
	})
	return found, err
}

// Revoke forgets every trust entry for project/rel and reports whether one
// existed.
func (t *TrustStore) Revoke(project, rel string) (bool, error) {
	return t.revoke(func(e TrustEntry) bool { return e.Project == project && e.Rel == rel })
}
