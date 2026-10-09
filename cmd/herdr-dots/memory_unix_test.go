//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

// The memory editor tests drive a sh script as $EDITOR (sed, stat), so they
// run on Unix; on Windows the editor goes through cmd.exe
// (platform.EditorCommand, tested in internal/platform).

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

// A dot pack's proposal can be edited from the CLI like any other entry. It
// stays labelled imported (and proposed) until the developer approves it.
func TestMemoryEditOfAnImportedDotProposal(t *testing.T) {
	c, out, _ := newMemCLI(t)
	if _, err := c.store.ImportPack(testPID, "shared", []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Use zod for validation"}}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/zod/valibot/' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "shared")
	if !strings.Contains(out.String(), "applied: 0 added, 1 updated, 0 forgotten") {
		t.Fatalf("%s", out)
	}
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared"})
	if len(es) != 1 || es[0].Text != "Use valibot for validation" || es[0].Source != memory.SourceImport || es[0].Status != memory.StatusProposed {
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

// Final review minor 8: edit works in a private 0700 directory that is
// removed afterwards with whatever the editor left in it (swap, backup).
func TestMemoryEditRemovesItsTempDirectory(t *testing.T) {
	c, _, _ := newMemCLI(t)
	t.Setenv("TMPDIR", t.TempDir())
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
	record := filepath.Join(t.TempDir(), "record")
	script := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nd=$(dirname \"$1\")\necho \"$d\" > " + record + "\n" +
		"(stat -f %Lp \"$d\" 2>/dev/null || stat -c %a \"$d\") >> " + record + "\n" +
		"touch \"$d/.swp-leftover\"\nsed -i.bak 's/zod/valibot/' \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(data))
	if len(lines) != 2 {
		t.Fatalf("record %q", data)
	}
	dir, mode := lines[0], lines[1]
	if filepath.Clean(dir) == filepath.Clean(os.Getenv("TMPDIR")) {
		t.Fatalf("edit must use its own directory, got the shared temp dir %s", dir)
	}
	if mode != "700" {
		t.Fatalf("temp directory mode %s, want 700", mode)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp directory %s survived the edit (err %v)", dir, err)
	}
}
