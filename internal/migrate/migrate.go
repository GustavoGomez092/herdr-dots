// Package migrate versions herdr-dots's persisted files and upgrades old ones.
//
// Every whole-file JSON or TOML state and config file has a schema version: a
// top-level "version" key, where a missing key means version 1. In 1.0 every
// kind is at version 1, so nothing is rewritten yet; the framework is here so
// a later format change (or a rename of the plugin) can upgrade files without
// losing data. Run, at engine start:
//
//   - leaves files at the current version alone;
//   - upgrades older files through registered migrations, one version at a
//     time, after backing the original up as <file>.bak-v<old>;
//   - marks files a newer binary wrote read-only for this process
//     (fsutil.ProtectNewer), so they are read but never overwritten;
//   - reports corrupt files and leaves them to their own loaders.
//
// Append-only JSONL logs (the feed, the memory ledger, history) are not
// versioned per file: their records only ever gain optional fields, and a
// breaking change must use a new file name (see CONTRIBUTING.md).
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

// Format is how a kind of file is encoded.
type Format string

const (
	JSON Format = "json"
	TOML Format = "toml"
)

// Root is the directory a kind's glob is relative to.
type Root int

const (
	ConfigRoot  Root = iota // the plugin config dir (user-editable)
	StateRoot               // the plugin state dir, shared by every Herdr session
	SessionRoot             // one Herdr session's state subdirectory
)

// Kind is one kind of persisted file.
type Kind struct {
	Name    string // "queue", "memory-scope", ...
	Root    Root
	Globs   []string // filepath.Glob patterns under Root, slash-separated
	Format  Format
	Current int // the newest version this binary reads and writes
	// Lock returns the lock file that guards file, or "" for none. State-root
	// files are shared by every session's engine, so a migration can only be
	// registered for a state-root kind that has a lock.
	Lock func(file string) string
	// SelfGuarded kinds refuse newer versions in their own loader (memory,
	// schedule, daily, trust), so a read-only check (Options.DryRun) can skip
	// them.
	SelfGuarded bool
	// Secret kinds hold keys (credentials.toml). A TOML or JSON parse error
	// quotes the offending line, so their errors are never reported or logged.
	Secret bool
}

// Migration upgrades one kind of file from version From to From+1. Apply
// gets the whole file and must return it with "version" set to From+1.
type Migration struct {
	Kind  string
	From  int
	Apply func(data []byte) ([]byte, error)
}

// Set is the kinds and migrations one binary knows.
type Set struct {
	Kinds      []Kind
	Migrations []Migration
}

// Roots are the three directories kinds live under. An empty root skips its
// kinds (the migrate command runs one session at a time).
type Roots struct{ Config, State, Session string }

// RootsOf is the roots for one engine: config, state and its own session.
func RootsOf(p paths.Paths) Roots {
	return Roots{Config: p.ConfigDir, State: p.StateDir, Session: p.SessionDir}
}

// Options tune Run.
type Options struct {
	// DryRun reads versions only: newer files are still marked read-only,
	// files that need a migration are reported as Pending, nothing is written.
	DryRun      bool
	LockTimeout time.Duration // per locked file; default 2s
	Logf        func(string, ...any)
}

// Report says what Run found. Paths are absolute.
type Report struct {
	Migrated []string // upgraded to the current version
	Pending  []string // would be upgraded (DryRun)
	ReadOnly []string // written by a newer version, or a failed migration
	Failed   []string // "<path>: <why>"; the file was left as it was
	Corrupt  []string // "<path>: <why>"; unreadable version, left to its loader
	Backups  []string // originals saved before a rewrite
}

// Empty reports whether Run found nothing to say.
func (r Report) Empty() bool {
	return len(r.Migrated)+len(r.Pending)+len(r.ReadOnly)+len(r.Failed)+len(r.Corrupt) == 0
}

// Summary is one log line.
func (r Report) Summary() string {
	if r.Empty() {
		return "all files current"
	}
	var parts []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			parts = append(parts, fmt.Sprintf("%s %d (%s)", label, len(items), strings.Join(items, "; ")))
		}
	}
	add("migrated", r.Migrated)
	add("pending", r.Pending)
	add("read-only", r.ReadOnly)
	add("failed", r.Failed)
	add("corrupt", r.Corrupt)
	return strings.Join(parts, ", ")
}

// Probe returns a file's schema version: its top-level "version" key, or 1
// when there is none. A "version" that is not a whole number (a map-shaped
// file keyed by pane id can't hold one) also means 1.
func Probe(data []byte, f Format) (int, error) {
	switch f {
	case JSON:
		var top map[string]json.RawMessage
		if err := json.Unmarshal(data, &top); err != nil {
			return 0, err
		}
		raw, ok := top["version"]
		if !ok {
			return 1, nil
		}
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return 1, nil
		}
		i, err := n.Int64()
		if err != nil {
			return 1, nil
		}
		return checkVersion(i)
	case TOML:
		var top map[string]any
		if _, err := toml.Decode(string(data), &top); err != nil {
			return 0, err
		}
		i, ok := top["version"].(int64)
		if !ok {
			return 1, nil
		}
		return checkVersion(i)
	}
	return 0, fmt.Errorf("unknown format %q", f)
}

func checkVersion(i int64) (int, error) {
	if i < 1 || i > 1<<20 {
		return 0, fmt.Errorf("version %d out of range", i)
	}
	return int(i), nil
}

// Validate checks that every migration chain is complete and safe.
func (s Set) Validate() error {
	kinds := map[string]Kind{}
	for _, k := range s.Kinds {
		if k.Name == "" || k.Current < 1 || len(k.Globs) == 0 {
			return fmt.Errorf("kind %q: needs a name, globs and a current version", k.Name)
		}
		if _, dup := kinds[k.Name]; dup {
			return fmt.Errorf("kind %q declared twice", k.Name)
		}
		kinds[k.Name] = k
	}
	steps := map[string]map[int]bool{}
	for _, m := range s.Migrations {
		k, ok := kinds[m.Kind]
		if !ok {
			return fmt.Errorf("migration for unknown kind %q", m.Kind)
		}
		if m.From < 1 || m.From >= k.Current || m.Apply == nil {
			return fmt.Errorf("kind %q: migration from %d is outside 1..%d", m.Kind, m.From, k.Current-1)
		}
		if k.Root == StateRoot && k.Lock == nil {
			return fmt.Errorf("kind %q: shared state needs a lock before it can be migrated", m.Kind)
		}
		if steps[m.Kind] == nil {
			steps[m.Kind] = map[int]bool{}
		}
		if steps[m.Kind][m.From] {
			return fmt.Errorf("kind %q: two migrations from %d", m.Kind, m.From)
		}
		steps[m.Kind][m.From] = true
	}
	for name, k := range kinds {
		for from := 1; from < k.Current; from++ {
			if !steps[name][from] {
				return fmt.Errorf("kind %q: no migration from %d to %d", name, from, from+1)
			}
		}
	}
	return nil
}

// Run checks every file of every kind under r. It returns an error only when
// the set itself is invalid; per-file problems go in the report.
func (s Set) Run(r Roots, o Options) (Report, error) {
	var rep Report
	if err := s.Validate(); err != nil {
		return rep, err
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = 2 * time.Second
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	for _, k := range s.Kinds {
		if o.DryRun && k.SelfGuarded {
			continue
		}
		root := map[Root]string{ConfigRoot: r.Config, StateRoot: r.State, SessionRoot: r.Session}[k.Root]
		if root == "" {
			continue
		}
		var files []string
		for _, g := range k.Globs {
			m, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(g)))
			if err != nil {
				return rep, fmt.Errorf("kind %q: %w", k.Name, err)
			}
			files = append(files, m...)
		}
		sort.Strings(files)
		for _, f := range files {
			s.one(k, f, o, &rep)
		}
	}
	return rep, nil
}

func (s Set) chain(kind string, from, to int) []Migration {
	var out []Migration
	for v := from; v < to; v++ {
		for _, m := range s.Migrations {
			if m.Kind == kind && m.From == v {
				out = append(out, m)
			}
		}
	}
	return out
}

// one handles a single file.
func (s Set) one(k Kind, file string, o Options, rep *Report) {
	data, err := os.ReadFile(file)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", file, err))
		}
		return
	}
	v, err := Probe(data, k.Format)
	switch {
	case err != nil:
		rep.Corrupt = append(rep.Corrupt, fmt.Sprintf("%s: %v", file, quiet(k, err)))
		return
	case v == k.Current:
		return
	case v > k.Current:
		fsutil.ProtectNewer(file)
		rep.ReadOnly = append(rep.ReadOnly, file)
		o.Logf("migrate: %s has version %d, this binary knows %d: read-only", file, v, k.Current)
		return
	case o.DryRun:
		rep.Pending = append(rep.Pending, file)
		return
	}
	upgrade := func() error { return s.upgrade(k, file, rep) }
	if k.Lock != nil {
		if lp := k.Lock(file); lp != "" {
			upgrade = withLock(lp, o.LockTimeout, upgrade)
		}
	}
	if err := upgrade(); err != nil {
		err = quiet(k, err)
		fsutil.ProtectNewer(file)
		rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", file, err))
		rep.ReadOnly = append(rep.ReadOnly, file)
		o.Logf("migrate: %s: %v (left as it was, read-only)", file, err)
		return
	}
	o.Logf("migrate: %s upgraded from version %d to %d", file, v, k.Current)
}

// quiet replaces the error of a Secret kind: its text may quote a key.
func quiet(k Kind, err error) error {
	if k.Secret {
		return errors.New("unreadable (details withheld: the file holds keys)")
	}
	return err
}

func withLock(lockPath string, timeout time.Duration, fn func() error) func() error {
	return func() error {
		l, err := lockfile.Acquire(lockPath, timeout)
		if err != nil {
			return fmt.Errorf("lock %s: %w", filepath.Base(lockPath), err)
		}
		defer l.Release()
		return fn()
	}
}

// upgrade re-reads file (another process may have upgraded it meanwhile),
// applies every step, checks the result, backs the original up and replaces
// the file atomically. On any error the file is untouched.
func (s Set) upgrade(k Kind, file string, rep *Report) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	st, err := os.Stat(file)
	if err != nil {
		return err
	}
	v, err := Probe(data, k.Format)
	if err != nil {
		return err
	}
	if v >= k.Current {
		return nil
	}
	out := data
	for _, m := range s.chain(k.Name, v, k.Current) {
		if out, err = m.Apply(out); err != nil {
			return fmt.Errorf("version %d to %d: %w", m.From, m.From+1, err)
		}
		got, err := Probe(out, k.Format)
		if err != nil || got != m.From+1 {
			return fmt.Errorf("version %d to %d produced version %d (%v)", m.From, m.From+1, got, err)
		}
	}
	backup, err := backupPath(file, v)
	if err != nil {
		return err
	}
	// The backup is as private as the file (an owner-only DACL on Windows):
	// credentials.toml and memory are backed up too.
	if err := fsutil.WriteFileAtomic(backup, data, 0o600); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(file, out, st.Mode().Perm()); err != nil {
		_ = os.Remove(backup)
		return err
	}
	rep.Backups = append(rep.Backups, backup)
	rep.Migrated = append(rep.Migrated, file)
	return nil
}

// backupPath is <file>.bak-v<version>, or with -2, -3 ... when that exists.
func backupPath(file string, v int) (string, error) {
	base := fmt.Sprintf("%s.bak-v%d", file, v)
	for i := 1; i < 100; i++ {
		p := base
		if i > 1 {
			p = fmt.Sprintf("%s-%d", base, i)
		}
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p, nil
		}
	}
	return "", fmt.Errorf("too many backups of %s", filepath.Base(file))
}
