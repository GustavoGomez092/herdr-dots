package fsutil

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Q9: the rename is made durable by syncing the parent directory after it.
func TestWriteFileAtomicSyncsTheParentDirAfterTheRename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	var synced []string
	old := syncDirFn
	syncDirFn = func(d string) {
		if got, _ := os.ReadFile(p); string(got) != "new" {
			d = "before the rename: " + d
		}
		synced = append(synced, d)
	}
	t.Cleanup(func() { syncDirFn = old })
	if err := WriteFileAtomic(p, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(synced, []string{dir}) {
		t.Fatalf("synced %q, want [%q]", synced, dir)
	}
}

func TestSyncDirIsBestEffort(t *testing.T) {
	syncDir(t.TempDir())                             // a real dir
	syncDir(filepath.Join(t.TempDir(), "gone", "x")) // missing: nothing to sync, no panic
}
