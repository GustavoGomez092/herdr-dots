//go:build windows

package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/platform"
)

// A fake claude.cmd records the command line it received. Purpose text full
// of cmd.exe metacharacters must reach the prompt file, never argv.
func TestClaudeThroughACmdShimKeepsThePromptOffTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	shim := "@echo off\r\necho %* > \"%~dp0args.txt\"\r\necho {\"is_error\":false,\"result\":\"\"}\r\n"
	if err := os.WriteFile(filepath.Join(dir, "claude.cmd"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := resolveBin
	resolveBin = platform.LookPath
	defer func() { resolveBin = old }()
	r := req()
	r.Cwd = dir // req's "/repo" doesn't exist here: CreateProcess fails with "The directory name is invalid"
	r.Purpose = `Flag "quotes" & pipes | 100% %PATH% carets ^ and <angle> brackets! & echo INJECTED`
	cmd := ClaudeCLI{Environ: os.Environ}.BuildCmd(r)
	defer cmd.Close()
	if _, _, err := (ExecRunner{}).Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	if !strings.Contains(got, "--append-system-prompt-file") || strings.Contains(got, "INJECTED") || strings.Contains(got, "disableAllHooks") {
		t.Fatalf("shim command line: %s", got)
	}
}
