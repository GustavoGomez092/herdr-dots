package memory

import "testing"

func TestStatsProposalsCountsPendingDotPackImports(t *testing.T) {
	s := Open(t.TempDir(), DefaultLimits())
	const p = "g-00000000000000aa"
	if _, err := s.ImportPack(p, "shared", []Entry{
		{Scope: ScopeDot, Kind: KindNote, Text: "Alpha rule about handlers."},
		{Scope: ScopeProject, Kind: KindConvention, Text: "Zebra convention about databases."},
	}); err != nil {
		t.Fatal(err)
	}
	// A restore's proposed entry (no From) is not a pack import.
	if _, err := s.Add(p, Entry{Scope: ScopeProject, Kind: KindNote, Text: "Quartz restored thing.", Status: StatusProposed, Source: SourceImport}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Proposals != 3 {
		t.Fatalf("Proposals = %d, want 3 (dot import + two project proposals)", st.Proposals)
	}
}
