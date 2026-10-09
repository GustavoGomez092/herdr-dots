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

var (
	roMu sync.RWMutex
	ro   = map[string]bool{}
)

func roKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// ProtectNewer makes path read-only for the rest of this process.
func ProtectNewer(path string) {
	roMu.Lock()
	defer roMu.Unlock()
	ro[roKey(path)] = true
}

// Protected reports whether ProtectNewer marked path.
func Protected(path string) bool {
	roMu.RLock()
	defer roMu.RUnlock()
	return ro[roKey(path)]
}

// ResetProtected forgets every mark. Tests use it.
func ResetProtected() {
	roMu.Lock()
	defer roMu.Unlock()
	ro = map[string]bool{}
}

// CheckWritable returns an error wrapping ErrNewerVersion for a protected
// path. WriteFileAtomic calls it; code that appends to a file calls it itself.
func CheckWritable(path string) error {
	if Protected(path) {
		return fmt.Errorf("%s: %w", filepath.Base(path), ErrNewerVersion)
	}
	return nil
}
