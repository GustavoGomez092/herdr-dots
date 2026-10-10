package memui

import (
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

func TestUnpinOfArchivedResearchSaysResearchGivesWay(t *testing.T) {
	st, n := fullStore(t, 2), 0
	st.Limits.MaxDot = 3
	if _, err := st.Add(pid, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindResearch,
		Text: "Research on caching", Source: memory.SourceModel, Status: memory.StatusActive, Pinned: true}); err != nil {
		t.Fatal(err)
	}
	st.Limits.MaxDot = 2
	m := selectRow(t, open(st, "security", &n), "Research on caching")
	v := press(m, "p").View()
	want := "unpinned d:m3, then archived: this dot's memory is full and research notes give way first"
	if !strings.Contains(flat(v), want) || strings.Contains(v, "imports") { // the status wraps to the popup
		t.Fatalf("want %q in:\n%s", want, v)
	}
}
