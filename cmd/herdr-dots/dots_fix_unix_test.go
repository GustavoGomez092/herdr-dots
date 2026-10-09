//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"herdr-dots/internal/memory"
)

func editWith(t *testing.T, c *memCLI, sedExpr string) error {
	t.Helper()
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak '"+sedExpr+"' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	return c.run([]string{"edit", "--scope", "dot", "--dot", "shared"})
}

func importedDismissed(t *testing.T, c *memCLI) {
	t.Helper()
	if _, err := c.store.ImportPack(testPID, "shared", []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindDismissed, Text: "Never raise CSRF"}}); err != nil {
		t.Fatal(err)
	}
}

// P9-R8: an import never suppresses repeats. Flipping its status in the
// editor must not relabel it as the developer's words.
func TestMemoryEditCannotApproveAnImportByStatusAlone(t *testing.T) {
	c, _, _ := newMemCLI(t)
	importedDismissed(t, c)
	err := editWith(t, c, `s/status = "proposed"/status = "active"/`)
	if err == nil || !strings.Contains(err.Error(), "memory approve") {
		t.Fatalf("want a refusal pointing to memory approve, got %v", err)
	}
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared"})
	if len(es) != 1 || es[0].Source != memory.SourceImport || es[0].Status != memory.StatusProposed {
		t.Fatalf("%+v", es)
	}
}

func TestMemoryEditOfAnImportsKindKeepsItImported(t *testing.T) {
	c, _, _ := newMemCLI(t)
	importedDismissed(t, c)
	if err := editWith(t, c, `s/kind = "dismissed"/kind = "note"/`); err != nil {
		t.Fatal(err)
	}
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared"})
	if len(es) != 1 || es[0].Source != memory.SourceImport || es[0].Status != memory.StatusProposed {
		t.Fatalf("%+v", es)
	}
}

func TestMemoryEditOfAnImportsTextMakesItTheDevelopersOwn(t *testing.T) {
	c, _, _ := newMemCLI(t)
	importedDismissed(t, c)
	if err := editWith(t, c, `s/Never raise CSRF/Never raise XSS/`); err != nil {
		t.Fatal(err)
	}
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared"})
	if len(es) != 1 || es[0].Source != memory.SourceUser || es[0].Status != memory.StatusActive || es[0].Text != "Never raise XSS" {
		t.Fatalf("%+v", es)
	}
}

func TestImportRefusesAPackThatIsNotARegularFile(t *testing.T) {
	r := newDotsCLI(t)
	fifo := filepath.Join(r.repo, "pack.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no fifo:", err)
	}
	done := make(chan error, 1)
	go func() { done <- r.c.run([]string{"import", fifo, "--yes"}) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("import blocked on a FIFO")
	}
}
