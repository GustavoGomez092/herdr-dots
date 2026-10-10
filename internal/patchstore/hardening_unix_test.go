//go:build !windows

package patchstore

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

// D1: Diff checks the patch file is a regular file of the drafted size
// before reading it, so a swapped-in file (here a FIFO with no writer, which
// a plain read would block on forever) is ErrChanged at once.
func TestDiffChecksTheFileBeforeReadingIt(t *testing.T) {
	s := testStore(t)
	if err := s.PutPatch(Meta{ID: id1, Status: StatusReady}, []byte("diff --git a/x b/x\n+fixed\n")); err != nil {
		t.Fatal(err)
	}
	p := s.PatchPath(id1)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := s.Diff(id1)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrChanged) {
			t.Fatalf("Diff of a FIFO: %v, want ErrChanged", err)
		}
	case <-time.After(3 * time.Second):
		// Unblock the reader so the test binary can finish.
		if w, err := os.OpenFile(p, os.O_WRONLY, 0); err == nil {
			w.Close()
		}
		<-done
		t.Fatal("Diff blocked reading a patch file that is not a regular file")
	}
}
