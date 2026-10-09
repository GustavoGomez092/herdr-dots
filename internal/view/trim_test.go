package view

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

func bigPane(id string, cards, feed, bodyLen int) Pane {
	p := Pane{PaneID: id}
	for i := 0; i < cards; i++ {
		p.Cards = append(p.Cards, queue.Card{ID: fmt.Sprintf("%s-c%03d", id, i), PaneID: id, Status: queue.StatusPending,
			Body: strings.Repeat("x", bodyLen), CreatedAt: time.Unix(int64(i), 0)})
	}
	for i := 0; i < feed; i++ {
		p.Feed = append(p.Feed, state.FeedEntry{PaneID: id, Turn: i, Outcome: "passed", Detail: strings.Repeat("d", 200)})
	}
	return p
}

func jsonSize(t *testing.T, s Snapshot) int {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

func TestTrimCapsCardsAndFeedPerPane(t *testing.T) {
	s := Snapshot{Panes: []Pane{bigPane("w1:p1", 150, 80, 10)}}
	got := Trim(s, MaxSnapshotBytes)
	p := got.Panes[0]
	if len(p.Cards) != MaxCardsPerPane || p.Cards[0].ID != "w1:p1-c000" || p.Cards[99].ID != "w1:p1-c099" {
		t.Fatalf("cards: %d, first %s", len(p.Cards), p.Cards[0].ID)
	}
	if len(p.Feed) != MaxFeedPerPane || p.Feed[0].Turn != 30 || p.Feed[49].Turn != 79 {
		t.Fatalf("feed keeps the newest 50: %d from turn %d", len(p.Feed), p.Feed[0].Turn)
	}
	if len(s.Panes[0].Cards) != 150 || len(s.Panes[0].Feed) != 80 {
		t.Fatal("Trim modified its input")
	}
}

func TestTrimStaysUnderTheByteBudgetWithoutCuttingACard(t *testing.T) {
	s := Snapshot{Panes: []Pane{bigPane("w1:p1", 100, 50, 6000), bigPane("w1:p2", 100, 50, 6000), bigPane("w1:p3", 3, 50, 10)}}
	got := Trim(s, MaxSnapshotBytes)
	if n := jsonSize(t, got); n > MaxSnapshotBytes {
		t.Fatalf("trimmed snapshot is %d bytes", n)
	}
	for _, p := range got.Panes {
		for _, c := range p.Cards {
			if len(c.Body) != len(s.Panes[0].Cards[0].Body) && p.PaneID != "w1:p3" {
				t.Fatalf("card %s was cut", c.ID)
			}
		}
		if len(p.Feed) > minFeedPerPane {
			t.Fatalf("feed of %s not shrunk first: %d", p.PaneID, len(p.Feed))
		}
	}
	if len(got.Panes[2].Cards) != 3 {
		t.Fatal("the small pane lost cards while the big ones had more")
	}
	if a, b := len(got.Panes[0].Cards), len(got.Panes[1].Cards); a < 1 || a-b > 1 || b-a > 1 {
		t.Fatalf("cards dropped unevenly: %d and %d", a, b)
	}
}

func TestTrimKeepsOneCardPerPaneEvenOverBudget(t *testing.T) {
	s := Snapshot{Panes: []Pane{bigPane("w1:p1", 5, 0, 300<<10)}}
	got := Trim(s, MaxSnapshotBytes)
	if len(got.Panes[0].Cards) != 1 || len(got.Panes[0].Cards[0].Body) != 300<<10 {
		t.Fatalf("floor: %d cards", len(got.Panes[0].Cards))
	}
}

// projectLibrary is roots×64 trusted and untrusted project dots, each with a
// 64 KB purpose (Phase 9's per-file and per-repository limits).
func projectLibrary(roots int) []LibraryDot {
	var lib []LibraryDot
	for r := 0; r < roots; r++ {
		for i := 0; i < 64; i++ {
			root := fmt.Sprintf("/work/repo%d", r)
			lib = append(lib, LibraryDot{Slug: fmt.Sprintf("dot-%02d", i), Name: fmt.Sprintf("Dot %02d", i), Origin: "project",
				Trusted: i%2 == 0, Clamped: []string{"write"}, Requested: []string{"research", "write"},
				ProjectID: fmt.Sprintf("proj%d", r), ProjectName: fmt.Sprintf("repo%d", r), Path: root + "/.herdr-dots/d.toml",
				SHA256: strings.Repeat("ab", 32), Rel: ".herdr-dots/d.toml", TrustedAt: "~/elsewhere", Root: root,
				Purpose: "Review the diff.\n" + strings.Repeat("é", 32<<10)})
		}
	}
	return lib
}

// checkLibrary: every entry is kept, every field but Purpose is untouched,
// and each purpose is a valid UTF-8 prefix of the original within limit
// bytes plus the ellipsis.
func checkLibrary(t *testing.T, got, want []LibraryDot, limit int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("library entries dropped: %d of %d", len(got), len(want))
	}
	for i := range got {
		g, w := got[i], want[i]
		if !utf8.ValidString(g.Purpose) || len(g.Purpose) > limit+len("…") ||
			!strings.HasPrefix(w.Purpose, strings.TrimSuffix(g.Purpose, "…")) || !strings.HasPrefix(g.Purpose, "Review the diff.") {
			t.Fatalf("purpose of %s: %d bytes, valid %v", g.Slug, len(g.Purpose), utf8.ValidString(g.Purpose))
		}
		g.Purpose, w.Purpose = "", ""
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("a field that gates actions changed:\n got %+v\nwant %+v", g, w)
		}
	}
}

// Phase 9 snapshots carry trusted project dots' purposes, up to 64 KB each:
// Trim shortens that display text, never the trust state, hashes or
// capability lists the UIs gate trust and runs on.
func TestTrimBoundsLibraryPurposesAndKeepsTrustFields(t *testing.T) {
	s := Snapshot{Library: projectLibrary(1), Panes: []Pane{bigPane("w1:p1", 3, 5, 10)}}
	got := Trim(s, MaxSnapshotBytes)
	if n := jsonSize(t, got); n > MaxSnapshotBytes {
		t.Fatalf("trimmed snapshot is %d bytes", n)
	}
	checkLibrary(t, got.Library, s.Library, maxLibraryPurpose)
	if len(got.Panes[0].Cards) != 3 || len(got.Panes[0].Feed) != 5 {
		t.Fatal("a pane under budget lost cards or feed for the library")
	}
	if len(s.Library[0].Purpose) != len("Review the diff.\n")+64<<10 {
		t.Fatal("Trim modified its input")
	}
}

// With many repositories the library alone would overflow: purposes shrink to
// a line before a single card is dropped.
func TestTrimShrinksPurposesBeforeDroppingCards(t *testing.T) {
	s := Snapshot{Library: projectLibrary(4), Panes: []Pane{bigPane("w1:p1", 20, 50, 10)}}
	got := Trim(s, MaxSnapshotBytes)
	if n := jsonSize(t, got); n > MaxSnapshotBytes {
		t.Fatalf("trimmed snapshot is %d bytes", n)
	}
	checkLibrary(t, got.Library, s.Library, minLibraryPurpose)
	if len(got.Panes[0].Cards) != 20 {
		t.Fatalf("cards dropped while purposes could shrink: %d", len(got.Panes[0].Cards))
	}
}

// A kept card is the card as queued: a digest keeps every part (SendCardsSeen
// compares the part count the developer saw) and a patch card its patch id,
// diff stat and flags.
func TestTrimKeepsDigestPartsAndPatchFields(t *testing.T) {
	p := bigPane("w1:p1", 60, 50, 6000)
	digest := queue.Card{ID: "dg", PaneID: "w1:p1", Kind: queue.KindDigest, Status: queue.StatusPending, Severity: "critical",
		Parts: []queue.CardPart{{Dot: "a", Body: strings.Repeat("a", 3000)}, {Dot: "b", Body: strings.Repeat("b", 3000)}, {Dot: "c", Failed: true}}}
	patch := queue.Card{ID: "pt", PaneID: "w1:p1", Kind: queue.KindPatch, Status: queue.StatusPending, Severity: "critical",
		PatchID: "draft-1", DiffStat: "3 files +42 −7", PatchFlags: []string{"binary", queue.FlagConflicts}, Body: strings.Repeat("p", 4000)}
	p.Cards = append([]queue.Card{digest, patch}, p.Cards...)
	s := Snapshot{Panes: []Pane{p, bigPane("w1:p2", 60, 50, 6000)}}
	got := Trim(s, MaxSnapshotBytes)
	if n := jsonSize(t, got); n > MaxSnapshotBytes {
		t.Fatalf("trimmed snapshot is %d bytes", n)
	}
	cards := got.Panes[0].Cards
	if len(cards) >= len(p.Cards) {
		t.Fatal("the test needs cards dropped")
	}
	for i, c := range cards {
		if !reflect.DeepEqual(c, p.Cards[i]) {
			t.Fatalf("card %s changed", c.ID)
		}
	}
}
