package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireFreeLockIsImmediate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "memory.lock")
	l, err := Acquire(p, 0)
	if err != nil {
		t.Fatalf("Acquire on a free lock: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireWaitsForRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "memory.lock")
	held, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		held.Release()
		close(released)
	}()
	start := time.Now()
	l, err := Acquire(p, 3*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()
	<-released
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("Acquire returned before the holder released the lock")
	}
}

func TestAcquireTimesOutWithErrLocked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "memory.lock")
	held, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	start := time.Now()
	if _, err := Acquire(p, 120*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if el := time.Since(start); el < 100*time.Millisecond || el > 2*time.Second {
		t.Fatalf("timed out after %v, want about 120ms", el)
	}
}
