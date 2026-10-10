package memory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// M10: an id is "m" and a number without a sign or leading zeros, the only
// form the store hands out.
func TestParseRefRefusesNonCanonicalIDs(t *testing.T) {
	for _, s := range []string{"p:m007", "p:m0", "p:m+3", "p:m-3", "p:m03"} {
		if _, err := ParseRef(s, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseRef(%q) = %v, want ErrInvalid", s, err)
		}
	}
	for _, s := range []string{"p:m3", "p:m10", "p:m100"} {
		if _, err := ParseRef(s, ""); err != nil {
			t.Errorf("ParseRef(%q): %v", s, err)
		}
	}
}

// M20: a hand-edited id with a sign or leading zeros is given a fresh id like
// any entry without one, so two entries never share an id. Every entry is
// kept with its text, kind, status and source; a canonical id never changes.
func TestHandEditedNonCanonicalIDsAreRenumberedNotLost(t *testing.T) {
	s := testStore(t)
	path := s.scopePath(pid, ScopeDot, "security")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"next":5,"entries":[
{"id":"m5","kind":"note","status":"active","source":"user","text":"canonical five","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
{"id":"m+5","kind":"finding","status":"active","source":"outcome","text":"signed five","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
{"id":"m005","kind":"dismissed","status":"active","source":"user","text":"padded five","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
{"id":"m-2","kind":"note","status":"archived","source":"model","text":"negative two","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}
]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusActive, StatusProposed, StatusArchived}})
	want := map[string]struct {
		kind   Kind
		status Status
		source Source
	}{
		"canonical five": {KindNote, StatusActive, SourceUser},
		"signed five":    {KindFinding, StatusActive, SourceOutcome},
		"padded five":    {KindDismissed, StatusActive, SourceUser},
		"negative two":   {KindNote, StatusArchived, SourceModel},
	}
	if len(es) != len(want) {
		t.Fatalf("entries %+v", es)
	}
	ids := map[string]bool{}
	for _, e := range es {
		w, ok := want[e.Text]
		if !ok || e.Kind != w.kind || e.Status != w.status || e.Source != w.source {
			t.Errorf("entry changed: %+v", e)
		}
		if ids[e.ID] {
			t.Errorf("duplicate id %s", e.ID)
		}
		ids[e.ID] = true
		if _, err := ParseRef("d:"+e.ID, "security"); err != nil {
			t.Errorf("entry %q has an id no ref can name: %s", e.Text, e.ID)
		}
		if e.Text == "canonical five" && e.ID != "m5" {
			t.Errorf("a canonical id changed: %s", e.ID)
		}
	}
}

// M20: prepare refuses an unknown source instead of storing it.
func TestAddRefusesAnUnknownSource(t *testing.T) {
	s := testStore(t)
	if _, err := s.Add(pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "x", Source: "someone"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err %v", err)
	}
}
