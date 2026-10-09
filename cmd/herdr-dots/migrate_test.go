package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/migrate"
	"herdr-dots/internal/paths"
)

func migratePaths(t *testing.T) paths.Paths {
	t.Helper()
	base := t.TempDir()
	p := paths.New(filepath.Join(base, "config"), filepath.Join(base, "state"), "/tmp/herdr-test.sock")
	for _, d := range []string{p.ConfigDir, p.SessionDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateCommandReportsAndSkipsRunningSessions(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.ConfigFile(), "default_backend = \"claude-cli\"\n")
	writeTestFile(t, p.QueueFile(), `{"version":2,"cards":[]}`) // from a newer herdr-dots
	other := filepath.Join(p.StateDir, "0123456789ab")
	writeTestFile(t, filepath.Join(other, "queue.json"), `{"next":0,"cards":[]}`)
	held, err := lockfile.TryLock(filepath.Join(other, "engine.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	var out bytes.Buffer
	if err := migrateCmd([]string{"--dry-run"}, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "read-only 1") || !strings.Contains(got, "queue.json") {
		t.Fatalf("the newer queue must be reported:\n%s", got)
	}
	if !strings.Contains(got, "skipped 1 running session(s) (0123456789ab)") {
		t.Fatalf("a running session must be skipped:\n%s", got)
	}
}

// The session-root kinds (queue, attachments, usage, patch-meta) have no lock
// of their own: engine.lock guards them. The migrate command must hold it
// while it migrates a session, so an engine starting meanwhile can't open
// the files mid-rewrite, and must release it afterwards.
func TestMigrateCommandHoldsTheSessionEngineLock(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"cards":[]}`) // version 1
	lock := p.EngineLock()
	heldDuringApply := false
	set := migrate.Set{
		Kinds: []migrate.Kind{{Name: "queue", Root: migrate.SessionRoot, Globs: []string{"queue.json"}, Format: migrate.JSON, Current: 2}},
		Migrations: []migrate.Migration{{Kind: "queue", From: 1, Apply: func([]byte) ([]byte, error) {
			heldDuringApply = lockfile.Held(lock)
			return []byte(`{"version":2,"cards":[]}`), nil
		}}},
	}
	var out bytes.Buffer
	if err := migrateWith(set, nil, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	if !heldDuringApply {
		t.Fatal("engine.lock must be held while a session is migrated")
	}
	if lockfile.Held(lock) {
		t.Fatal("engine.lock must be released after the session is migrated")
	}
	if !strings.Contains(out.String(), "migrated 1") || !strings.Contains(out.String(), "backup: ") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// credentials.toml holds keys: a parse error quotes the offending line, so
// neither the command nor the engine log may ever repeat it.
func TestMigrateNeverPrintsCredentialsContents(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	const secret = "sk-ant-THISMUSTNEVERLEAK0123456789"
	writeTestFile(t, filepath.Join(p.ConfigDir, "credentials.toml"), "[anthropic]\napi_key = \""+secret+"\n")
	var out bytes.Buffer
	if err := migrateCmd(nil, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	var logs []string
	startupMigrations(p, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	all := out.String() + strings.Join(logs, "\n")
	if !strings.Contains(all, "credentials.toml") {
		t.Fatalf("the unreadable file should still be named:\n%s", all)
	}
	if strings.Contains(all, secret) || strings.Contains(all, "api_key") {
		t.Fatalf("credentials contents leaked:\n%s", all)
	}
}

func TestReadOnlyNoticeNamesTheFiles(t *testing.T) {
	if readOnlyNotice(migrate.Report{}) != "" {
		t.Fatal("no notice without read-only files")
	}
	n := readOnlyNotice(migrate.Report{ReadOnly: []string{"/s/queue.json", "/c/dots/security.toml"}})
	if !strings.Contains(n, "2 file(s)") || !strings.Contains(n, "queue.json, security.toml") {
		t.Fatalf("notice %q", n)
	}
}

func TestWritingCommandsProtectNewerFiles(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"version":3,"cards":[]}`)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", p.ConfigDir)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", p.StateDir)
	t.Setenv("HERDR_SOCKET_PATH", p.SocketPath)
	protectNewerFiles("pane")
	if fsutil.Protected(p.QueueFile()) {
		t.Fatal("the pane writes no state: nothing to protect")
	}
	protectNewerFiles("manage")
	if !fsutil.Protected(p.QueueFile()) {
		t.Fatal("manage must not overwrite a newer queue")
	}
}

// Every process that writes persisted files outside the engine runs the gate
// first: credentials (keys), dots edit/import, setup-mcp, the memory
// commands, the manage UI and uninstall-extras.
func TestEveryWritingCommandRunsTheGate(t *testing.T) {
	p := migratePaths(t)
	creds := filepath.Join(p.ConfigDir, "credentials.toml")
	writeTestFile(t, creds, "version = 2\n[anthropic]\napi_key = \"x\"\n")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", p.ConfigDir)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", p.StateDir)
	t.Setenv("HERDR_SOCKET_PATH", p.SocketPath)
	for _, cmd := range []string{"manage", "keys", "dots", "setup-mcp", "memory", "uninstall-extras"} {
		fsutil.ResetProtected()
		protectNewerFiles(cmd)
		if !fsutil.Protected(creds) {
			t.Errorf("%s must not overwrite a newer credentials.toml", cmd)
		}
	}
	fsutil.ResetProtected()
}

func TestStartupMigrationsLogsTheSummary(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.UsageFile(), `{"version":5}`)
	var logs []string
	rep := startupMigrations(p, func(f string, a ...any) { logs = append(logs, f) })
	if len(rep.ReadOnly) != 1 || len(logs) == 0 {
		t.Fatalf("report %+v logs %v", rep, logs)
	}
}
