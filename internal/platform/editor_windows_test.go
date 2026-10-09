//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An editor given as a command line with a flag gets the file as one quoted
// argument, even with spaces in both paths.
func TestEditorCommandRunsTheEditorLineOnTheFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(dir, "my editor.cmd")
	if err := os.WriteFile(editor, []byte("@echo off\r\necho [%1] [%2] > \"%~dp0seen.txt\"\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "memory file.toml")
	if err := EditorCommand(`"`+editor+`" --wait`, file).Run(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "seen.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(string(got)); s != `[--wait] ["`+file+`"]` {
		t.Fatalf("editor saw %s", s)
	}
}
