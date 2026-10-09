package view

import (
	"encoding/json"
	"unicode/utf8"
)

// Snapshot budgets (spec F9). The engine streams a snapshot to every UI on
// each change, so one must stay small however long a session runs.
const (
	MaxSnapshotBytes = 256 << 10
	MaxCardsPerPane  = 100
	MaxFeedPerPane   = 50
	minFeedPerPane   = 5

	// A library entry's purpose is display text (the profile shows its first
	// line); trusted project dots can carry up to 64 KB each (Phase 9).
	maxLibraryPurpose = 1 << 10
	minLibraryPurpose = 160
)

// Trim bounds s for broadcasting. Each pane keeps its first MaxCardsPerPane
// cards in queue order (most severe, then oldest) and its newest
// MaxFeedPerPane feed entries, and each library purpose is cut to 1 KB.
// While the JSON is still over maxBytes, feeds shrink to 5 entries, library
// purposes to 160 bytes, then the pane with the most cards drops its last
// card, one at a time, down to one card per pane.
//
// Only display text is shortened, never what the UIs gate actions on. A card
// is shown whole or not at all: the UIs act on cards they show, so no card
// text is ever cut, and a kept digest keeps every part (SendCardsSeen checks
// the part count the developer saw). Library entries are never dropped, and
// only their purpose is cut: trust state, hashes, paths and capability lists
// stay as built. Trim does not modify s.
func Trim(s Snapshot, maxBytes int) Snapshot {
	out := s
	out.Panes = make([]Pane, len(s.Panes))
	for i, p := range s.Panes {
		if len(p.Cards) > MaxCardsPerPane {
			p.Cards = p.Cards[:MaxCardsPerPane]
		}
		if len(p.Feed) > MaxFeedPerPane {
			p.Feed = p.Feed[len(p.Feed)-MaxFeedPerPane:]
		}
		out.Panes[i] = p
	}
	out.Library = cutPurposes(s.Library, maxLibraryPurpose)
	if size(out) <= maxBytes {
		return out
	}
	for i := range out.Panes {
		if f := out.Panes[i].Feed; len(f) > minFeedPerPane {
			out.Panes[i].Feed = f[len(f)-minFeedPerPane:]
		}
	}
	out.Library = cutPurposes(s.Library, minLibraryPurpose)
	// Measure once, then subtract each dropped card (and its comma).
	for total := size(out); total > maxBytes; {
		most := -1
		for i, p := range out.Panes {
			if len(p.Cards) > 1 && (most < 0 || len(p.Cards) > len(out.Panes[most].Cards)) {
				most = i
			}
		}
		if most < 0 {
			break // one card per pane is the floor
		}
		c := out.Panes[most].Cards
		total -= size(c[len(c)-1]) + 1
		out.Panes[most].Cards = c[:len(c)-1]
	}
	return out
}

// cutPurposes copies lib with each purpose cut to at most n bytes (plus an
// ellipsis), on a rune boundary.
func cutPurposes(lib []LibraryDot, n int) []LibraryDot {
	if lib == nil {
		return nil
	}
	out := make([]LibraryDot, len(lib))
	for i, d := range lib {
		if len(d.Purpose) > n {
			j := n
			for j > 0 && !utf8.RuneStart(d.Purpose[j]) {
				j--
			}
			d.Purpose = d.Purpose[:j] + "…"
		}
		out[i] = d
	}
	return out
}

func size(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}
