package memory

import (
	"fmt"
	"strings"
	"testing"
)

// A proposed entry that names its pack goes through ImportPack: it keeps its
// From, stays proposed, and the pending-import cap applies.
func TestImportEntriesRoutesPackProposalsThroughImportPack(t *testing.T) {
	s := testStore(t)
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima", "mike",
		"november", "oscar", "papa", "quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray", "yankee", "zulu"}
	var es []Entry
	for i := 0; i < MaxPendingImports+10; i++ {
		// Distinct words so the store's near-duplicate merge keeps them apart.
		text := fmt.Sprintf("Crafted %s %s %s rule", words[i%26], words[(i/26+7)%26], words[(i*5+3)%26])
		es = append(es, Entry{Scope: ScopeProject, Kind: KindConvention, Text: text, Status: StatusProposed, Source: SourceUser, From: "team", ID: "m9"})
	}
	res := s.ImportEntries(pid, es)
	got := list(t, s, pid, Filter{Scope: ScopeProject})
	if len(got) != MaxPendingImports || res.Added < len(got) {
		t.Fatalf("%d landed (added %d), want the limit %d", len(got), res.Added, MaxPendingImports)
	}
	for _, e := range got {
		if e.Status != StatusProposed || e.Source != SourceImport || e.From != "team" {
			t.Fatalf("a pack proposal must stay proposed with From kept: %+v", e)
		}
	}
	if len(res.Skipped) == 0 || !strings.HasPrefix(res.Skipped[0], "skipped (pack team) ") {
		t.Fatalf("the cap's refusals are reported per pack: %q", res.Skipped)
	}
}

// Everything else goes through Add as an import, with a fresh id.
func TestImportEntriesAddsPlainEntriesAsImports(t *testing.T) {
	s := testStore(t)
	res := s.ImportEntries(pid, []Entry{{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "Use zod for validation", Status: StatusActive, Source: SourceUser, ID: "m77"}})
	got := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if res.Added != 1 || len(res.Skipped) != 0 || len(got) != 1 {
		t.Fatalf("res %+v entries %+v", res, got)
	}
	if got[0].Source != SourceImport || got[0].Status != StatusActive || got[0].ID == "m77" {
		t.Fatalf("a plain entry is added as SourceImport with a new id: %+v", got[0])
	}
}

// An all-global import needs no project; anything else does.
func TestImportEntriesGlobalNeedsNoProject(t *testing.T) {
	s := testStore(t)
	global := []Entry{{Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: "Prefer Go for tooling", Status: StatusActive}}
	if NeedsProject(global) {
		t.Fatal("global entries need no project")
	}
	if NeedsProject(nil) {
		t.Fatal("no entries need no project")
	}
	if !NeedsProject(append(global, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "x"})) {
		t.Fatal("a dot entry needs a project")
	}
	res := s.ImportEntries("", global)
	if res.Added != 1 || len(res.Skipped) != 0 {
		t.Fatalf("%+v", res)
	}
	if got := list(t, s, "", Filter{Scope: ScopeGlobal, Dot: "security"}); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

// Skipped lines name the entry or the pack, never the entry's text.
func TestImportEntriesSkippedLinesNeverQuoteTheText(t *testing.T) {
	s := testStore(t)
	long := "PRIVATEMARKER " + strings.Repeat("word ", 120)
	res := s.ImportEntries("", []Entry{
		{Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: long, Status: StatusActive},
		{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "PRIVATEMARKER pack text", Status: StatusProposed, From: "team"},
		{Scope: ScopeProject, Kind: KindNote, Text: "PRIVATEMARKER plain", Status: StatusActive},
	})
	if res.Added != 0 || len(res.Skipped) != 3 {
		t.Fatalf("%+v", res)
	}
	for _, l := range res.Skipped {
		if strings.Contains(l, "PRIVATEMARKER") {
			t.Fatalf("a skipped line quotes the entry text: %q", l)
		}
	}
	if !strings.HasPrefix(res.Skipped[0], "skipped entry 1: ") || !strings.Contains(res.Skipped[0], "too long") ||
		!strings.HasPrefix(res.Skipped[1], "skipped entry 3: ") ||
		res.Skipped[2] != "skipped the entries of pack security: they need a project" {
		t.Fatalf("%q", res.Skipped)
	}
}
