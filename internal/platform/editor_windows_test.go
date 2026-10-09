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

// A decoy <editor>.cmd in the directory herdr-dots was started from is not run:
// the editor line is searched in PATH, not in the current directory.
func TestEditorCommandIgnoresTheCurrentDirectory(t *testing.T) {
	work := t.TempDir()
	seen := filepath.Join(work, "decoy-ran.txt")
	if err := os.WriteFile(filepath.Join(work, "decoyed.cmd"), []byte("@echo off\r\necho x > \""+seen+"\"\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	tmp := t.TempDir()
	file := filepath.Join(tmp, "memory.toml")
	cmd := EditorCommand("decoyed", file)
	if cmd.Dir != tmp {
		t.Fatalf("editor starts in %q, want the private dir %q", cmd.Dir, tmp)
	}
	_ = cmd.Run() // "decoyed" is not on PATH, so the shell reports an error
	if _, err := os.Stat(seen); err == nil {
		t.Fatal("the decoy in the current directory ran")
	}
}
