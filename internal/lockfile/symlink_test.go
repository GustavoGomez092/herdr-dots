package lockfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSymlinkedLockFileIsRefusedAndItsTargetUntouched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(target, []byte("precious data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "engine.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if l, err := TryLock(link); err == nil {
		l.Release()
		t.Fatal("TryLock followed a symlinked lock file")
	}
	if Held(link) {
		t.Fatal("a refused lock file is not a held lock")
	}
	if l, err := Acquire(link, 20*time.Millisecond); err == nil {
		l.Release()
		t.Fatal("Acquire followed a symlinked lock file")
	}
	if b, _ := os.ReadFile(target); string(b) != "precious data\n" {
		t.Fatalf("the target was overwritten: %q", b)
	}
}
