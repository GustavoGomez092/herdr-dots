package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The post-rc Setup screen shares setup-mcp's and setup-keys' plan and apply
// steps. These goldens were recorded from the commands before that refactor:
// the CLI output must stay byte-identical. Set UPDATE_SETUP_GOLDEN=1 to
// re-record them (only for a deliberate wording change).
//
// The goldens hold Unix paths and shell quoting, so they run on Unix only.

var backupStampRe = regexp.MustCompile(`\.bak-[0-9]{8}-[0-9]{6}(-[0-9]+)?`)

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "setup-golden", name+".txt")
	if os.Getenv("UPDATE_SETUP_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (record it with UPDATE_SETUP_GOLDEN=1)", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func skipGoldenOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the goldens hold Unix paths and shell quoting")
	}
}

// mcpGolden runs setupMCP and returns its output (and error) with the temp
// home replaced by $HOME.
func (r *mcpSetupRig) golden(args ...string) string {
	r.out.Reset()
	err := setupMCP(args, r.io)
	out := strings.ReplaceAll(r.out.String(), r.home, "$HOME")
	if err != nil {
		out += "ERROR: " + strings.ReplaceAll(err.Error(), r.home, "$HOME") + "\n"
	}
	return out
}

func TestSetupMCPOutputIsUnchanged(t *testing.T) {
	skipGoldenOnWindows(t)
	r := newMCPSetupRig(t, "claude", "codex")
	if err := os.MkdirAll(filepath.Dir(r.codexConfig()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.codexConfig(), []byte("model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.io.env.ClaudeConfigPath(), []byte(`{"numStartups": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	r.io.in = strings.NewReader("n\n")
	b.WriteString("## cancel\n" + r.golden())
	r.io.in = strings.NewReader("y\n")
	b.WriteString("## install both\n" + r.golden())
	b.WriteString("## again, codex only\n" + r.golden("--client", "codex"))
	r.io.run = func(a []string) (string, error) {
		if len(a) > 2 && a[0] == "claude" && a[2] == "add" {
			return "add failed\n", errors.New("exit status 1")
		}
		return "ok\n", nil
	}
	b.WriteString("## claude fails\n" + r.golden("--yes"))
	r.claudeHasEntry(t)
	r.io.run = func(a []string) (string, error) { return "removed\n", nil }
	b.WriteString("## remove both\n" + r.golden("--remove", "--yes"))
	b.WriteString("## unknown client\n" + r.golden("--client", "vim"))
	r.io.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	_ = os.Remove(r.codexConfig())
	b.WriteString("## no clients\n" + r.golden())
	checkGolden(t, "setup-mcp", backupStampRe.ReplaceAllString(b.String(), ".bak-STAMP"))
}

// captureStdio runs f with os.Stdin reading in and os.Stdout captured.
func captureStdio(t *testing.T, in string, f func() error) string {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.WriteString(inW, in); inW.Close() }()
	done := make(chan string)
	go func() { b, _ := io.ReadAll(outR); done <- string(b) }()
	os.Stdin, os.Stdout = inR, outW
	ferr := f()
	os.Stdin, os.Stdout = oldIn, oldOut
	outW.Close()
	out := <-done
	inR.Close()
	if ferr != nil {
		out += "ERROR: " + ferr.Error() + "\n"
	}
	return out
}

func TestSetupKeysOutputIsUnchanged(t *testing.T) {
	skipGoldenOnWindows(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "herdr", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_CONFIG_PATH", cfg)
	t.Setenv("HERDR_BIN_PATH", filepath.Join(dir, "missing-herdr"))
	real, err := filepath.EvalSymlinks(dir) // keys.Append writes through symlinks (macOS /var)
	if err != nil {
		t.Fatal(err)
	}
	norm := func(s string) string {
		s = strings.ReplaceAll(strings.ReplaceAll(s, real, "$DIR"), dir, "$DIR")
		return backupStampRe.ReplaceAllString(s, ".bak-STAMP")
	}
	run := func(in string, args ...string) string {
		return norm(captureStdio(t, in, func() error { return runSetupKeys(args) }))
	}
	var b strings.Builder
	b.WriteString("## no config, cancel\n" + run("n\n"))
	b.WriteString("## no config, yes, reload fails\n" + run("", "--yes"))
	b.WriteString("## all there\n" + run("", "--yes"))

	fake := filepath.Join(dir, "fake-herdr")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho reloaded\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fake)
	partial := "[keys]\nprefix = \"ctrl+b\"\n\n[[keys.command]]\nkey = \"prefix+a\"\ntype = \"shell\"\ncommand = \"htop\"\n\n" +
		"[[keys.command]]\nkey = \"prefix+m\"\ntype = \"plugin_action\"\ncommand = \"herdr-dots.manage\"\n"
	if err := os.WriteFile(cfg, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	b.WriteString("## partial, answer y, reload ok\n" + run("y\n"))
	if err := os.WriteFile(cfg, []byte("[keys\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.WriteString("## invalid toml\n" + run("", "--yes"))
	checkGolden(t, "setup-keys", b.String())
}
