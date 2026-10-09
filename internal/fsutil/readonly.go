package fsutil

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

// ErrNewerVersion refuses a write to a file that a newer herdr-dots wrote. Its
// schema version is higher than this binary knows, so rewriting it would drop
// data the newer version relies on. internal/migrate marks such files when a
// process starts; this process then only reads them.
var ErrNewerVersion = errors.New("written by a newer herdr-dots: kept read-only")

// ErrMigrationFailed refuses a write to a file whose migration failed (a lock
// that stayed busy, a step that errored). The file is left as it was: this
// process can't write it in the old format without losing the upgrade.
var ErrMigrationFailed = errors.New("not written")

var (
	roMu sync.RWMutex
	ro   = map[string]func(base string) error{}
)

func roKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// ProtectNewer makes path read-only for the rest of this process.
func ProtectNewer(path string) {
	protect(path, func(base string) error { return fmt.Errorf("%s: %w", base, ErrNewerVersion) })
}

// ProtectFailed makes path read-only for the rest of this process because
// its migration failed; why is the reason, which every refused write repeats.
// It must not quote file contents (credentials.toml).
func ProtectFailed(path, why string) {
	protect(path, func(base string) error {
		return fmt.Errorf("migration of %s failed: %s; %w", base, why, ErrMigrationFailed)
	})
}

func protect(path string, refuse func(base string) error) {
	roMu.Lock()
	defer roMu.Unlock()
	ro[roKey(path)] = refuse
}

// Protected reports whether ProtectNewer or ProtectFailed marked path.
func Protected(path string) bool {
	roMu.RLock()
	defer roMu.RUnlock()
	return ro[roKey(path)] != nil
}

// ResetProtected forgets every mark. Tests use it.
func ResetProtected() {
	roMu.Lock()
	defer roMu.Unlock()
	ro = map[string]func(string) error{}
}

// CheckWritable returns an error for a protected path: one wrapping
// ErrNewerVersion, or ErrMigrationFailed with the reason. WriteFileAtomic
// calls it; code that appends to a file calls it itself.
func CheckWritable(path string) error {
	roMu.RLock()
	refuse := ro[roKey(path)]
	roMu.RUnlock()
	if refuse != nil {
		return refuse(filepath.Base(path))
	}
	return nil
}
