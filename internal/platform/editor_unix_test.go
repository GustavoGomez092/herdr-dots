//go:build unix

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditorCommandPassesTheFileAsAnArgument(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "my memory; echo INJECTED.toml")
	out := filepath.Join(dir, "out.txt")
	cmd := EditorCommand("printf '%s' >"+out, file)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != file {
		t.Fatalf("editor saw %q, want %q", got, file)
	}
}
