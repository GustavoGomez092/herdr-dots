package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveAllButKeepsOnlyTheNamedEntry(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"memory.lock", "meta.json", "dots/security.json"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeAllBut(dir, "memory.lock"); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "memory.lock" {
		t.Fatalf("left %v", ents)
	}
}
