package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/platform/platformtest"
)

// Every test in this package sees "claude not installed" unless it says
// otherwise, so a developer's own claude.cmd never changes argv expectations.
func init() { resolveBin = func(string) (string, error) { return "", exec.ErrNotFound } }

// withBin makes resolveBin report path for the duration of the test.
func withBin(t *testing.T, path string) {
	t.Helper()
	old := resolveBin
	resolveBin = func(string) (string, error) { return path, nil }
	t.Cleanup(func() { resolveBin = old })
}

func longReq() Request {
	r := req()
	r.Purpose = strings.Repeat("Check every auth path for token leaks. ", 130) // > 4,000 characters
	return r
}

func TestShortPromptStaysInArgvExactlyAsV1(t *testing.T) {
	r := req()
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	defer cmd.Close()
	want := []string{"-p", "--output-format", "json", "--no-session-persistence", "--strict-mcp-config",
		"--settings", researchSettings(nil, ""), "--append-system-prompt", SystemPrompt(r),
		"--model", "sonnet", "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob",
		"--setting-sources", "user"} // security review R1: never the repository's settings
	if !slices.Equal(cmd.Args, want) || cmd.Cleanup != nil {
		t.Fatalf("args %q cleanup %v", cmd.Args, cmd.Cleanup != nil)
	}
}

func TestLongPromptMovesToAPrivateFile(t *testing.T) {
	r := longReq()
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	path, ok := argAfter(cmd.Args, "--append-system-prompt-file")
	if !ok || slices.Contains(cmd.Args, "--append-system-prompt") || slices.Contains(cmd.Args, SystemPrompt(r)) {
		t.Fatalf("args %q", cmd.Args)
	}
	if v, _ := argAfter(cmd.Args, "--settings"); v != researchSettings(nil, "") {
		t.Fatalf("settings %q: no shim, so inline", v)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != SystemPrompt(r) {
		t.Fatalf("prompt file: %v (%d bytes)", err, len(got))
	}
	platformtest.AssertPrivateFile(t, path)
	cmd.Close()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Close left %s: %v", path, err)
	}
}

func TestShimGetsPromptAndSettingsAsFiles(t *testing.T) {
	withBin(t, `C:\Users\x\AppData\Roaming\npm\claude.cmd`)
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(req())
	defer cmd.Close()
	settings, _ := argAfter(cmd.Args, "--settings")
	prompt, ok := argAfter(cmd.Args, "--append-system-prompt-file")
	if !ok || settings == researchSettings(nil, "") {
		t.Fatalf("args %q", cmd.Args)
	}
	if b, err := os.ReadFile(settings); err != nil || string(b) != researchSettings(nil, "") {
		t.Fatalf("settings file %q %v", b, err)
	}
	for _, a := range cmd.Args {
		if strings.ContainsAny(a, "\"%!\n") {
			t.Fatalf("argument %q can't pass through cmd.exe", a)
		}
	}
	if _, err := os.Stat(prompt); err != nil {
		t.Fatal(err)
	}
}

// fileRunner checks the prompt file exists while the command "runs".
type fileRunner struct {
	sawFile bool
	stdout  string
	err     error
	wait    bool
}

func (f *fileRunner) Run(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	if p, ok := argAfter(c.Args, "--append-system-prompt-file"); ok {
		_, err := os.Stat(p)
		f.sawFile = err == nil
	}
	if f.wait {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return []byte(f.stdout), nil, f.err
}

func TestRunDeletesThePromptFileOnSuccessErrorAndTimeout(t *testing.T) {
	ok := `{"is_error":false,"result":"{\"relevant\":false}","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`
	for name, fr := range map[string]*fileRunner{
		"success": {stdout: ok},
		"error":   {err: errors.New("exit status 1")},
		"timeout": {wait: true},
	} {
		before, _ := os.ReadDir(os.TempDir())
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, _ = ClaudeCLI{Runner: fr, Environ: func() []string { return nil }}.Run(ctx, longReq())
		cancel()
		after, _ := os.ReadDir(os.TempDir())
		if !fr.sawFile {
			t.Errorf("%s: the prompt file must exist during the run", name)
		}
		if leftover := runDirs(after) - runDirs(before); leftover > 0 {
			t.Errorf("%s: %d herdr-dots-run-* dirs left behind", name, leftover)
		}
	}
}

func runDirs(es []os.DirEntry) int {
	n := 0
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "herdr-dots-run-") {
			n++
		}
	}
	return n
}

type errRunner struct{ err error }

func (e errRunner) Run(context.Context, Cmd) ([]byte, []byte, error) { return nil, nil, e.err }

func TestRunReportsShimPreflightErrorsVerbatim(t *testing.T) {
	tooLong := fmt.Errorf("%w for claude.cmd (9214 > 8191): install the native claude.exe or shorten the dot's backend args", platform.ErrCmdTooLong)
	_, err := ClaudeCLI{Runner: errRunner{tooLong}, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if !errors.Is(err, platform.ErrCmdTooLong) || !strings.Contains(err.Error(), "claude.exe") {
		t.Fatalf("claude err = %v", err)
	}
	_, err = CodexCLI{Runner: errRunner{platform.ErrUnsafeBatchArg}, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if !errors.Is(err, platform.ErrUnsafeBatchArg) {
		t.Fatalf("codex err = %v", err)
	}
}
