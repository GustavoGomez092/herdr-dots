package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/paths"
)

// mcpSetupRig fakes every outside effect of setup-mcp. Nothing here reads or
// writes the real ~/.claude.json or ~/.codex: the home is a temp dir.
type mcpSetupRig struct {
	io   setupIO
	out  *bytes.Buffer
	ran  [][]string
	home string
	p    paths.Paths
}

func newMCPSetupRig(t *testing.T, onPath ...string) *mcpSetupRig {
	t.Helper()
	r := &mcpSetupRig{out: &bytes.Buffer{}, home: t.TempDir()}
	r.p = paths.New(filepath.Join(r.home, "cfg"), filepath.Join(r.home, "state"), "")
	if err := os.MkdirAll(r.p.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	r.io = setupIO{
		in:  strings.NewReader("y\n"),
		out: r.out,
		run: func(a []string) (string, error) {
			r.ran = append(r.ran, a)
			return "ok: " + strings.Join(a[:3], " ") + "\n", nil
		},
		lookPath: func(bin string) (string, error) {
			for _, b := range onPath {
				if b == bin {
					return "/usr/bin/" + bin, nil
				}
			}
			return "", errors.New("not found")
		},
		env:   mcpinstall.Env{Getenv: func(string) string { return "" }, Home: r.home},
		now:   func() time.Time { return time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC) },
		exe:   filepath.Join(r.home, "opt", "herdr-dots", "bin", "herdr-dots"), // absolute on every OS
		paths: func() (paths.Paths, error) { return r.p, nil },
	}
	return r
}

func (r *mcpSetupRig) codexConfig() string { return r.io.env.CodexConfigPath() }

func TestSetupMCPInstallsForClaudeAfterConfirmation(t *testing.T) {
	r := newMCPSetupRig(t, "claude")
	if err := setupMCP([]string{"--client", "claude"}, r.io); err != nil {
		t.Fatal(err)
	}
	out := r.out.String()
	if !strings.Contains(out, "Will run: claude mcp add --scope user --env 'HERDR_PANE_ID=${HERDR_PANE_ID:-}'") ||
		!strings.Contains(out, "Apply these changes? [y/N]") || !strings.Contains(out, "bypass the Dots approval queue") {
		t.Fatalf("output:\n%s", out)
	}
	if len(r.ran) != 3 || r.ran[1][2] != "add" || strings.Join(r.ran[2], " ") != "claude mcp get herdr-dots" {
		t.Fatalf("ran %v", r.ran)
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Claude Code" {
		t.Fatalf("status %q", got)
	}
}

func TestSetupMCPAnythingButYesCancels(t *testing.T) {
	r := newMCPSetupRig(t, "claude", "codex")
	r.io.in = strings.NewReader("\n")
	if err := setupMCP(nil, r.io); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 0 || mcpFileExists(r.codexConfig()) || mcpFileExists(mcpinstall.RecordPath(r.p.ConfigDir)) {
		t.Fatalf("cancel must change nothing: ran %v", r.ran)
	}
	if !strings.Contains(r.out.String(), "Cancelled. Nothing changed.") {
		t.Fatalf("output:\n%s", r.out.String())
	}
}

func TestSetupMCPMissingClaudeIsAClearErrorWithNoEdits(t *testing.T) {
	r := newMCPSetupRig(t, "codex")
	err := setupMCP([]string{"--client", "claude", "--yes"}, r.io)
	if err == nil || !strings.Contains(err.Error(), "claude CLI not found on PATH") {
		t.Fatalf("got %v", err)
	}
	if len(r.ran) != 0 || mcpFileExists(r.codexConfig()) {
		t.Fatal("no partial edits")
	}
}

func TestSetupMCPBothSkipsAMissingClientAndInstallsCodex(t *testing.T) {
	r := newMCPSetupRig(t, "codex")
	if err := setupMCP([]string{"--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(r.codexConfig())
	if err != nil || !strings.Contains(string(b), "tool_timeout_sec = 180") || !strings.Contains(string(b), `env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]`) {
		t.Fatalf("codex config %q %v", b, err)
	}
	if !strings.Contains(r.out.String(), "Skipping Claude Code") || !strings.Contains(r.out.String(), "$ codex mcp list") {
		t.Fatalf("output:\n%s", r.out.String())
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Codex" {
		t.Fatalf("status %q", got)
	}

	// Remove restores the (empty) config and drops the record.
	r.out.Reset()
	if err := setupMCP([]string{"--remove", "--client", "codex", "--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(r.codexConfig()); len(b) != 0 {
		t.Fatalf("after remove: %q", b)
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: not installed (run herdr-dots setup-mcp)" {
		t.Fatalf("status %q", got)
	}
}

func TestSetupMCPRefusesGoRunBinaries(t *testing.T) {
	r := newMCPSetupRig(t, "claude")
	r.io.exe = filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "herdr-dots")
	if err := setupMCP([]string{"--yes"}, r.io); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Fatalf("got %v", err)
	}
}

func (r *mcpSetupRig) claudeHasEntry(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(r.io.env.ClaudeConfigPath(), []byte(`{"mcpServers": {"herdr-dots": {"type": "stdio"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSetupMCPRemoveBothContinuesPastAFailingClaude(t *testing.T) {
	r := newMCPSetupRig(t, "claude", "codex")
	if err := setupMCP([]string{"--client", "codex", "--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	r.claudeHasEntry(t)
	path := mcpinstall.RecordPath(r.p.ConfigDir)
	rs, _ := mcpinstall.LoadRecords(path)
	if err := mcpinstall.SaveRecords(path, mcpinstall.WithRecord(rs, mcpinstall.Record{Client: mcpinstall.ClientClaude, Scope: "user"})); err != nil {
		t.Fatal(err)
	}
	r.io.run = func(a []string) (string, error) {
		if len(a) > 2 && a[0] == "claude" && a[2] == "remove" {
			return "boom\n", errors.New("exit status 1")
		}
		return "ok\n", nil
	}
	err := setupMCP([]string{"--remove", "--yes"}, r.io)
	if err == nil || !strings.Contains(err.Error(), "Claude Code") {
		t.Fatalf("a real failure must be reported: %v", err)
	}
	if b, _ := os.ReadFile(r.codexConfig()); len(b) != 0 {
		t.Fatalf("codex must still be removed: %q", b)
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Claude Code" {
		t.Fatalf("status %q: the failed client keeps its record, the removed one loses it", got)
	}
}

func TestSetupMCPRemoveWithNothingInstalledForClaudeIsQuiet(t *testing.T) {
	r := newMCPSetupRig(t, "claude", "codex")
	if err := setupMCP([]string{"--client", "codex", "--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	r.ran = nil
	if err := setupMCP([]string{"--remove", "--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	for _, a := range r.ran {
		if a[0] == "claude" {
			t.Fatalf("claude must not be called when nothing is installed: %v", r.ran)
		}
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: not installed (run herdr-dots setup-mcp)" {
		t.Fatalf("status %q", got)
	}
}

func TestSetupMCPInstallBothKeepsCodexWhenClaudeFails(t *testing.T) {
	r := newMCPSetupRig(t, "claude", "codex")
	r.io.run = func(a []string) (string, error) {
		if len(a) > 2 && a[0] == "claude" && a[2] == "add" {
			return "", errors.New("exit status 1")
		}
		return "ok\n", nil
	}
	if err := setupMCP([]string{"--yes"}, r.io); err == nil {
		t.Fatal("the failed add must be reported")
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Codex" {
		t.Fatalf("status %q", got)
	}
}

func TestSetupMCPRejectsAnUnknownClient(t *testing.T) {
	r := newMCPSetupRig(t, "claude")
	if err := setupMCP([]string{"--client", "cursor"}, r.io); err == nil {
		t.Fatal("unknown client accepted")
	}
}

// A record file a newer herdr-dots wrote fails fast, before the y/N prompt:
// no client config may change when the record can't be saved afterwards.
func TestSetupMCPRefusesANewerRecordFileBeforeAsking(t *testing.T) {
	r := newMCPSetupRig(t, "claude", "codex")
	rec := mcpinstall.RecordPath(r.p.ConfigDir)
	const newer = `{"version":2,"installs":[]}`
	if err := os.WriteFile(rec, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	err := setupMCP(nil, r.io)
	if !errors.Is(err, mcpinstall.ErrRecordsVersion) || !strings.Contains(err.Error(), "nothing changed") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(r.out.String(), "Apply these changes?") || len(r.ran) != 0 || mcpFileExists(r.codexConfig()) {
		t.Fatalf("nothing may be planned or changed: ran %v\n%s", r.ran, r.out.String())
	}
	if b, _ := os.ReadFile(rec); string(b) != newer {
		t.Fatalf("the newer record changed: %s", b)
	}
}
