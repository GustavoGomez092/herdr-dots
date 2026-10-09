package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

const testPID = "g-00000000000000aa"

func newMemCLI(t *testing.T) (*memCLI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	c := &memCLI{
		store: memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits()),
		resolve: func(string) (memory.Project, error) {
			return memory.Project{ID: testPID, Name: "repo", Root: repo}, nil
		},
		cwd: repo, out: out, errOut: errOut, in: strings.NewReader(""),
	}
	return c, out, errOut
}

func mustRun(t *testing.T, c *memCLI, args ...string) {
	t.Helper()
	if err := c.run(args); err != nil {
		t.Fatalf("memory %v: %v", args, err)
	}
}

func TestMemoryCommandIsRegistered(t *testing.T) {
	if commands["memory"] == nil {
		t.Fatal(`"memory" must be in the commands map`)
	}
}

func TestMemoryListJSON(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "--kind", "note", "Use zod for validation")
	out.Reset()
	mustRun(t, c, "list", "--json")
	var items []map[string]any
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(items) != 1 {
		t.Fatalf("%s", out)
	}
	it := items[0]
	for k, want := range map[string]string{"ref": "d:m1", "id": "m1", "scope": "dot", "dot": "security", "kind": "note", "text": "Use zod for validation", "status": "active", "source": "user"} {
		if it[k] != want {
			t.Errorf("%s = %v, want %q", k, it[k], want)
		}
	}
}

func TestMemoryAddRefusesSecretsWithoutForce(t *testing.T) {
	c, out, errOut := newMemCLI(t)
	err := c.run([]string{"add", "--scope", "dot", "--dot", "security", "token = AbCdEf123456GhIj"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want a refusal mentioning --force, got %v", err)
	}
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "--force", "token = AbCdEf123456GhIj")
	if !strings.Contains(errOut.String(), "warning") || !strings.Contains(out.String(), "added d:m1") {
		t.Fatalf("out %q err %q", out, errOut)
	}
}

func TestMemoryAddValidatesScope(t *testing.T) {
	c, _, _ := newMemCLI(t)
	for _, args := range [][]string{
		{"add", "no scope"},
		{"add", "--scope", "team", "x"},
		{"add", "--scope", "dot", "needs a dot"},
		{"add", "--scope", "project"},
	} {
		if err := c.run(args); err == nil {
			t.Errorf("memory %v should fail", args)
		}
	}
}

func TestMemoryForgetScrub(t *testing.T) {
	c, _, _ := newMemCLI(t)
	if err := c.store.RecordOutcome(testPID, memory.Outcome{Dot: "security", Card: "7", Kind: memory.OutcomeDismissed, Body: "CSRF on /admin is intentionally omitted."}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "forget", "d:m1", "--dot", "security", "--scrub")
	if led, _ := c.store.Ledger(testPID); len(led) != 0 {
		t.Fatalf("ledger %+v", led)
	}
	if es, _ := c.store.List(testPID, memory.Filter{}); len(es) != 0 {
		t.Fatalf("entries %+v", es)
	}
}

func TestMemoryForgetAllNeedsYes(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "first fact here")
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "second unrelated thing")
	if err := c.run([]string{"forget-all", "--scope", "dot", "--dot", "security"}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want a --yes refusal, got %v", err)
	}
	mustRun(t, c, "forget-all", "--scope", "dot", "--dot", "security", "--yes")
	if !strings.Contains(out.String(), "forgot 2 entries") {
		t.Fatalf("%s", out)
	}
}

func TestMemoryPinApproveAndProjects(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "project", "Postgres is the only datastore")
	if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeProject, Dot: "security", Kind: memory.KindConvention, Text: "Use zod everywhere", Source: memory.SourceModel, Status: memory.StatusProposed}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "pin", "p:m1")
	mustRun(t, c, "approve", "p:m2")
	if err := c.run([]string{"approve", "p:m1"}); err == nil {
		t.Fatal("approving an active entry must fail")
	}
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeProject})
	if !es[0].Pinned || es[1].Status != memory.StatusActive {
		t.Fatalf("%+v", es)
	}
	out.Reset()
	mustRun(t, c, "projects")
	if !strings.Contains(out.String(), testPID) || !strings.Contains(out.String(), "repo") {
		t.Fatalf("projects:\n%s", out)
	}
}

func TestMemoryEditAppliesExactlyOneUpdate(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Sessions expire after one hour")
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/Use zod for validation/Use valibot for validation/' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	out.Reset()
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
	if !strings.Contains(out.String(), "applied: 0 added, 1 updated, 0 forgotten") {
		t.Fatalf("%s", out)
	}
	e, _ := c.store.Get(testPID, memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m1"})
	if e.Text != "Use valibot for validation" {
		t.Fatalf("%+v", e)
	}
	c.editor = ""
	if err := c.run([]string{"edit"}); err == nil || !strings.Contains(err.Error(), "EDITOR") {
		t.Fatalf("want an $EDITOR hint, got %v", err)
	}
}

func TestMemoryExportImportIsIdempotent(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
	mustRun(t, c, "add", "--scope", "project", "Postgres is the only datastore")
	out.Reset()
	mustRun(t, c, "export")
	file := filepath.Join(t.TempDir(), "memory.toml")
	if err := os.WriteFile(file, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "import", file)
	es, _ := c.store.List(testPID, memory.Filter{})
	if len(es) != 2 {
		t.Fatalf("import of an export must dedupe: %+v", es)
	}
}

func TestMemoryPurge(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "project", "Postgres is the only datastore")
	if err := c.run([]string{"purge"}); err == nil {
		t.Fatal("purge needs --project")
	}
	if err := c.run([]string{"purge", "--project", testPID}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("purge without --yes: %v", err)
	}
	mustRun(t, c, "purge", "--project", testPID, "--yes")
	if _, err := os.Stat(filepath.Join(c.store.Dir, testPID)); !os.IsNotExist(err) {
		t.Fatal("project directory survived purge")
	}
	out.Reset()
	mustRun(t, c, "projects")
	if !strings.Contains(out.String(), "no projects") {
		t.Fatalf("%s", out)
	}
}

func TestMemoryUnknownCommand(t *testing.T) {
	c, _, _ := newMemCLI(t)
	if err := c.run([]string{"frobnicate"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("%v", err)
	}
	if err := c.run(nil); err == nil {
		t.Fatal("no subcommand must print usage")
	}
}

// Added for the review context (not in the brief).

func importFile(t *testing.T, body string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "in.toml")
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

const secretImport = "version = 1\n[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\ntext = \"token = AbCdEf123456GhIj\"\n"

func TestMemoryImportSetsSourceImport(t *testing.T) {
	c, _, _ := newMemCLI(t)
	f := importFile(t, "version = 1\n[[entry]]\nscope = \"project\"\nkind = \"convention\"\nstatus = \"active\"\nsource = \"user\"\ntext = \"Postgres is the only datastore\"\n")
	mustRun(t, c, "import", f)
	es, _ := c.store.List(testPID, memory.Filter{})
	if len(es) != 1 || es[0].Source != memory.SourceImport {
		t.Fatalf("%+v", es)
	}
}

func TestMemoryImportSecretWarnsAndNeedsConfirmation(t *testing.T) {
	c, out, errOut := newMemCLI(t)
	f := importFile(t, secretImport)
	if err := c.run([]string{"import", f}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("non-interactive import of a secret must ask for --yes, got %v", err)
	}
	if es, _ := c.store.List(testPID, memory.Filter{}); len(es) != 0 {
		t.Fatalf("nothing may be stored: %+v", es)
	}
	mustRun(t, c, "import", f, "--yes")
	if !strings.Contains(errOut.String(), "warning") || !strings.Contains(out.String(), "imported 1 entries") {
		t.Fatalf("out %q err %q", out, errOut)
	}
}

func TestMemoryImportSecretInteractiveConfirm(t *testing.T) {
	c, _, _ := newMemCLI(t)
	c.interactive = true
	c.in = strings.NewReader("n\n")
	f := importFile(t, secretImport)
	if err := c.run([]string{"import", f}); err == nil {
		t.Fatal("declined import must fail")
	}
	c.in = strings.NewReader("y\n")
	mustRun(t, c, "import", f)
	if es, _ := c.store.List(testPID, memory.Filter{}); len(es) != 1 {
		t.Fatalf("%+v", es)
	}
}

func TestMemoryEditMarksUserSource(t *testing.T) {
	c, _, _ := newMemCLI(t)
	if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "Use zod for validation", Source: memory.SourceModel, Status: memory.StatusActive}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/zod/valibot/' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
	e, _ := c.store.Get(testPID, memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m1"})
	if e.Source != memory.SourceUser {
		t.Fatalf("source %q", e.Source)
	}
}

// Final review minor 4: a secret in paths is gated like a secret in text.
func TestMemorySecretInPathsIsGated(t *testing.T) {
	c, _, _ := newMemCLI(t)
	f := importFile(t, "version = 1\n[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\npaths = [\"deploy/AKIAIOSFODNN7EXAMPLE/**\"]\ntext = \"plain deploy note\"\n")
	if err := c.run([]string{"import", f}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("a secret in paths must need --yes, got %v", err)
	}
	if err := c.run([]string{"add", "--scope", "dot", "--dot", "security", "--paths", "deploy/AKIAIOSFODNN7EXAMPLE/**", "plain deploy note"}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("add with a secret in --paths must need --force, got %v", err)
	}
	if es, _ := c.store.List(testPID, memory.Filter{}); len(es) != 0 {
		t.Fatalf("nothing may be stored: %+v", es)
	}
}

func TestMemoryProjectsPrintsReadableAndErrors(t *testing.T) {
	c, out, errOut := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "project", "Postgres is the only datastore")
	bad := filepath.Join(c.store.Dir, "p-00000000000000bb")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "meta.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	mustRun(t, c, "projects")
	if !strings.Contains(out.String(), testPID) || !strings.Contains(errOut.String(), "warning") {
		t.Fatalf("out %q err %q", out, errOut)
	}
}
