package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-dots/internal/fsutil"
)

// scheduleFile is <project>/schedule.json: the last slot each scheduled dot
// claimed on this project. Engines in every Herdr session share it, so a slot
// runs once.
type scheduleFile struct {
	Version int                  `json:"version"`
	Last    map[string]time.Time `json:"last"`
}

func (s *Store) schedulePath(p string) string { return filepath.Join(s.Dir, p, "schedule.json") }

func (s *Store) readScheduleLocked(p string) (scheduleFile, error) {
	f := scheduleFile{Version: fileVersion, Last: map[string]time.Time{}}
	data, err := os.ReadFile(s.schedulePath(p))
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return scheduleFile{}, fmt.Errorf("%w: %s/schedule.json: %v", ErrCorrupt, p, err)
	}
	if f.Version > fileVersion {
		return scheduleFile{}, fmt.Errorf("%w (%s/schedule.json has version %d)", ErrVersion, p, f.Version)
	}
	if f.Last == nil {
		f.Last = map[string]time.Time{}
	}
	return f, nil
}

func checkProjectDot(p, dot string) error {
	if !ValidProjectID(p) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if !ValidSlug(dot) {
		return fmt.Errorf("%w: dot %q", ErrInvalid, dot)
	}
	return nil
}

// LastSchedule is the last slot dot claimed on project p (zero if none).
func (s *Store) LastSchedule(p, dot string) (time.Time, error) {
	if err := checkProjectDot(p, dot); err != nil {
		return time.Time{}, err
	}
	var last time.Time
	err := s.withReadLock(filepath.Join(s.Dir, p), func() error {
		f, err := s.readScheduleLocked(p)
		last = f.Last[dot]
		return err
	})
	return last, err
}

// ScheduleSkew is how far past now a stored claim may lie and still count. A
// later claim was written by a skewed clock: it is treated as absent, since it
// would otherwise refuse every slot before it (ruling P8-R12).
const ScheduleSkew = 2 * time.Minute

// ClaimSchedule records slot as dot's last run on p and reports true, or
// reports false when that slot (or a later one) was already claimed, by this
// engine or one in another Herdr session. A stored claim after now plus
// ScheduleSkew is treated as absent and overwritten (logged). A corrupt file is
// replaced: losing it can at most re-run one slot.
func (s *Store) ClaimSchedule(p, dot string, slot, now time.Time) (bool, error) {
	if err := checkProjectDot(p, dot); err != nil {
		return false, err
	}
	claimed := false
	err := s.withLock(filepath.Join(s.Dir, p), func() error {
		f, err := s.readScheduleLocked(p)
		if errors.Is(err, ErrCorrupt) {
			s.logf("memory: %v; starting a new schedule file", err)
			f, err = scheduleFile{Version: fileVersion, Last: map[string]time.Time{}}, nil
		}
		if err != nil {
			return err
		}
		if last, ok := f.Last[dot]; ok && !last.Before(slot) {
			if !last.After(now.Add(ScheduleSkew)) {
				return nil
			}
			s.logf("memory: %s/schedule.json: dot %s's claim %s is in the future (a clock was wrong); replacing it", p, dot, last.Format(time.RFC3339))
		}
		f.Last[dot] = slot
		data, err := json.MarshalIndent(f, "", "  ")
		if err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(s.schedulePath(p), append(data, '\n'), 0o600); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

// SeeDots records in p's meta.json that these dots are attached there now.
// Scheduled dots with projects = ["attached"] run on projects seen in the
// last 14 days. A missing meta.json is created with the id only (SeeProject
// adds the name and roots, and may run before or after this).
func (s *Store) SeeDots(p string, slugs []string, now time.Time) error {
	if !ValidProjectID(p) {
		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	dir := filepath.Join(s.Dir, p)
	path := filepath.Join(dir, "meta.json")
	return s.withLock(dir, func() error {
		m := ProjectMeta{Version: fileVersion, ID: p, CreatedAt: s.now()}
		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return err
		default:
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("%w: %s/meta.json: %v", ErrCorrupt, p, err)
			}
		}
		if m.Version > fileVersion {
			return fmt.Errorf("%w (%s has version %d)", ErrVersion, path, m.Version)
		}
		if m.DotsSeen == nil {
			m.DotsSeen = map[string]time.Time{}
		}
		for _, slug := range slugs {
			if ValidSlug(slug) {
				m.DotsSeen[slug] = now.UTC()
			}
		}
		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		return fsutil.WriteFileAtomic(path, append(out, '\n'), 0o600)
	})
}

// RecentDotProjects lists projects where dot was attached at or after since.
// Projects with unreadable metadata are skipped; their errors are returned
// joined, alongside the projects that could be read. A project whose
// meta.json SeeDots created but SeeProject hasn't filled in yet has no name,
// and its Roots may be empty.
func (s *Store) RecentDotProjects(dot string, since time.Time) ([]ProjectMeta, error) {
	all, err := s.Projects()
	var out []ProjectMeta
	for _, m := range all {
		if t, ok := m.DotsSeen[dot]; ok && !t.Before(since) {
			out = append(out, m)
		}
	}
	return out, err
}

// scrubDotProactiveLocked forgets a deleted dot's proactive state in project
// p: its dots_seen stamp and its schedule claim. A file that doesn't parse is
// reported, not edited. The caller holds the project lock.
func (s *Store) scrubDotProactiveLocked(p, dot string) error {
	var errs []error
	f, err := s.readScheduleLocked(p)
	if errors.Is(err, ErrCorrupt) {
		// It holds only slugs and times: replace it, as ClaimSchedule does, so
		// forgetting succeeds and a recreated dot inherits no stale claim.
		s.logf("memory: %v; replacing it", err)
		f.Version, f.Last = fileVersion, map[string]time.Time{dot: {}}
		err = nil
	}
	if err != nil {
		errs = append(errs, err)
	} else if _, ok := f.Last[dot]; ok {
		delete(f.Last, dot)
		data, err := json.MarshalIndent(f, "", "  ")
		if err == nil {
			err = fsutil.WriteFileAtomic(s.schedulePath(p), append(data, '\n'), 0o600)
		}
		errs = append(errs, err)
	}
	path := filepath.Join(s.Dir, p, "meta.json")
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		errs = append(errs, err)
	default:
		var m ProjectMeta
		if err := json.Unmarshal(data, &m); err != nil {
			errs = append(errs, fmt.Errorf("%w: %s/meta.json: %v", ErrCorrupt, p, err))
		} else if m.Version > fileVersion {
			errs = append(errs, fmt.Errorf("%w (%s has version %d)", ErrVersion, path, m.Version))
		} else if _, ok := m.DotsSeen[dot]; ok {
			delete(m.DotsSeen, dot)
			out, err := json.MarshalIndent(m, "", "  ")
			if err == nil {
				err = fsutil.WriteFileAtomic(path, append(out, '\n'), 0o600)
			}
			errs = append(errs, err)
		}
	}
	errs = append(errs, removeMetaAsidesLocked(filepath.Join(s.Dir, p), dot))
	return errors.Join(errs...)
}

// removeMetaAsidesLocked deletes meta.json.corrupt-* asides, and the
// migration backups meta.json.bak-v* and schedule.json.bak-v*, that mention
// the dot's slug as a key. An aside doesn't parse and a backup is an old
// format, so neither can be edited safely; they hold only a name, roots, dot
// stamps and schedule claims, so the whole file goes. Files that never name
// the dot are left alone.
func removeMetaAsidesLocked(dir, slug string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var errs []error
	// The dots_seen (or schedule "last") key form: "<slug>": (a name or root
	// that merely equals the slug, or a value, is followed by a comma or
	// brace, not a colon).
	needle := regexp.MustCompile(`"` + regexp.QuoteMeta(slug) + `"\s*:`)
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !(strings.HasPrefix(n, "meta.json.corrupt-") || strings.HasPrefix(n, "meta.json.bak-v") ||
			strings.HasPrefix(n, "schedule.json.bak-v")) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if needle.Match(data) {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
