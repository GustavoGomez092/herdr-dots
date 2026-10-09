package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

func TestSlugArgumentsAreValidatedBeforeAnyFileIsTouched(t *testing.T) {
	r := newDotsCLI(t)
	// A file a traversal would reach: <config>/x.toml next to the dots dir.
	os.MkdirAll(r.c.dotsDir, 0o700)
	os.WriteFile(filepath.Join(filepath.Dir(r.c.dotsDir), "x.toml"), []byte("name = \"X\"\n"), 0o600)
	for _, args := range [][]string{{"export", "../x"}, {"export", "../../x"}, {"profile", "../x"}, {"profile", "a/b"}} {
		err := r.c.run(args)
		if err == nil || !strings.Contains(err.Error(), "not a valid dot name") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestExportForceCountSubtractsTheDroppedEntries(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Security")
	d.Purpose = "p"
	r.saveDot(t, d)
	r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", Source: memory.SourceModel})
	r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "Keep handlers small.", Source: memory.SourceModel})
	pack := filepath.Join(r.repo, "s.toml")
	r.run(t, "export", "security", "--with-memory", "dot", "--force", "-o", pack)
	if got := r.errOut.String(); !strings.Contains(got, "with 1 memory entry to") {
		t.Fatalf("the count must not include the dropped entry:\n%s", got)
	}
}

func TestImportPreviewShowsArgsAndEffort(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Tuned")
	d.Purpose = "Tuned."
	d.Backend.Args = []string{"--effort", "low"}
	r.saveDot(t, d)
	r.run(t, "export", "tuned")
	pack := filepath.Join(r.repo, "t.toml")
	os.WriteFile(pack, r.out.Bytes(), 0o600)
	r.out.Reset()
	if err := r.c.run([]string{"import", pack, "--as", "Tuned Two"}); err == nil {
		t.Fatal("needs --yes")
	}
	if o := r.out.String(); !strings.Contains(o, "args:") || !strings.Contains(o, "--effort low") {
		t.Fatalf("preview:\n%s", o)
	}
}

func TestHelpFlagPrintsUsageAndSucceeds(t *testing.T) {
	r := newDotsCLI(t)
	for _, args := range [][]string{{"export", "-h"}, {"import", "-h"}, {"trust", "--help"}} {
		if err := r.c.run(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	if r.errOut.Len() == 0 {
		t.Fatal("no usage printed")
	}
	c, _, _ := newMemCLI(t)
	if err := c.run([]string{"list", "-h"}); err != nil {
		t.Fatalf("memory list -h: %v", err)
	}
}

func TestMemoryRestoreMessageSaysAddedOrMerged(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "import", importFile(t, "version = 1\n[[entry]]\nscope = \"project\"\nkind = \"convention\"\nstatus = \"active\"\ntext = \"Postgres only\"\n"))
	if !strings.Contains(out.String(), "added or merged 1 entries") {
		t.Fatalf("%s", out)
	}
}
