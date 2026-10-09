package main

import (
	"fmt"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

func packBackup(n int, from string) string {
	var b strings.Builder
	b.WriteString("version = 1\n")
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa", "quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray", "yankee", "zulu"}
	for i := 0; i < n; i++ {
		// Distinct words so the store's near-duplicate merge keeps them apart.
		text := fmt.Sprintf("Crafted %s %s %s rule", words[i%26], words[(i/26+7)%26], words[(i*5+3)%26])
		fmt.Fprintf(&b, "[[entry]]\nscope = \"dot\"\ndot = %q\nkind = \"note\"\nstatus = \"proposed\"\nsource = \"user\"\nfrom = %q\ntext = %q\n", from, from, text)
	}
	return b.String()
}

func TestMemoryRestoreCannotBypassThePendingImportLimit(t *testing.T) {
	c, out, errOut := newMemCLI(t)
	over := memory.MaxPendingImports + 10
	mustRun(t, c, "import", importFile(t, packBackup(over, "shared")))
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared"})
	if len(es) != memory.MaxPendingImports {
		t.Fatalf("%d pending imports landed, want the limit %d", len(es), memory.MaxPendingImports)
	}
	for _, e := range es {
		if e.Status != memory.StatusProposed || e.Source != memory.SourceImport {
			t.Fatalf("%+v", e)
		}
	}
	if !strings.Contains(errOut.String(), "skipped (pack shared)") || !strings.Contains(errOut.String(), "waiting for the developer") {
		t.Fatalf("out %q err %q", out, errOut)
	}
	if strings.Contains(errOut.String(), "Crafted ") {
		t.Fatal("a skip must not quote the entry text")
	}
}

func TestMemoryApproveLabelsImportedAndRestoredEntries(t *testing.T) {
	c, out, _ := newMemCLI(t)
	if _, err := c.store.ImportPack(testPID, "shared", []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "From a pack."}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, c, "import", importFile(t, "version = 1\n[[entry]]\nscope = \"dot\"\ndot = \"shared\"\nkind = \"note\"\nstatus = \"proposed\"\ntext = \"From my backup.\"\n"))
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared"})
	if len(es) != 2 {
		t.Fatalf("%+v", es)
	}
	for _, e := range es {
		out.Reset()
		mustRun(t, c, "approve", e.Ref().String(), "--dot", "shared", "--yes")
		want := "restored from a backup"
		if e.Text == "From a pack." {
			want = "imported from a shared dot pack (shared)"
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("%s: approve screen lacks %q:\n%s", e.Text, want, out)
		}
	}
}
