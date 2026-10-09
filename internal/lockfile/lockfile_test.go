package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSingleHolder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.lock")
	l, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !Held(p) {
		t.Fatal("Held should see the lock")
	}
	if _, err := TryLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if Held(p) {
		t.Fatal("released lock must not be held")
	}
	l2, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	l2.Release()
}
