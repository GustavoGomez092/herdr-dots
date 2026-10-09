package memory

import (
	"errors"
	"strings"
	"testing"
)

func tomlFixture() []Entry {
	return []Entry{
		{ID: "m3", Scope: ScopeProject, Kind: KindDecision, Text: "Auth tokens live in httpOnly cookies.", Status: StatusActive, Source: SourceUser, Pinned: true},
		{ID: "m17", Scope: ScopeDot, Dot: "security", Kind: KindDismissed, Text: "CSRF on /admin \"intentionally\" omitted.\nInternal tool.", Status: StatusActive, Source: SourceOutcome, Paths: []string{"internal/admin/**"}},
		{ID: "m2", Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: "Be terse.", Status: StatusActive, Source: SourceUser},
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	data, err := ExportTOML(tomlFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# herdr-dots memory.") || !strings.Contains(string(data), `ref = "d:m17"`) {
		t.Fatalf("export:\n%s", data)
	}
	back, err := ImportTOML(data)
	if err != nil {
		t.Fatal(err)
	}
	adds, updates, deletes := DiffTOML(tomlFixture(), back)
	if len(adds)+len(updates)+len(deletes) != 0 {
		t.Fatalf("an unedited round trip must be a no-op: %+v %+v %+v", adds, updates, deletes)
	}
	if back[1].Text != tomlFixture()[1].Text || back[1].Dot != "security" || back[1].Paths[0] != "internal/admin/**" {
		t.Fatalf("lost data: %+v", back[1])
	}
}

func TestDiffFindsAddsUpdatesDeletes(t *testing.T) {
	data, _ := ExportTOML(tomlFixture())
	edited := strings.Replace(string(data), "Be terse.", "Be very terse.", 1)
	cut := strings.Index(edited, "[[entry]]")
	second := cut + strings.Index(edited[cut+1:], "[[entry]]") + 1
	edited = edited[:cut] + edited[second:] // drop the first block (p:m3)
	edited += "\n[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\npinned = false\npaths = []\ntext = \"Brand new note.\"\n"
	back, err := ImportTOML([]byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	adds, updates, deletes := DiffTOML(tomlFixture(), back)
	if len(adds) != 1 || adds[0].Text != "Brand new note." || adds[0].ID != "" {
		t.Fatalf("adds %+v", adds)
	}
	if len(updates) != 1 || updates[0].Text != "Be very terse." || updates[0].Ref().String() != "g:m2" {
		t.Fatalf("updates %+v", updates)
	}
	if len(deletes) != 1 || deletes[0].String() != "p:m3" {
		t.Fatalf("deletes %+v", deletes)
	}
}

func TestChangedScopeBecomesAnAdd(t *testing.T) {
	data, _ := ExportTOML(tomlFixture()[2:])
	edited := strings.Replace(string(data), `scope = "global"`, `scope = "dot"`, 1)
	back, err := ImportTOML([]byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	adds, _, deletes := DiffTOML(tomlFixture()[2:], back)
	if len(adds) != 1 || len(deletes) != 1 {
		t.Fatalf("adds %+v deletes %+v", adds, deletes)
	}
}

func TestImportRejectsBadDocuments(t *testing.T) {
	for _, doc := range []string{
		"version = 1\n[[entry]]\nscope = \"team\"\nkind = \"note\"\ntext = \"x\"\n",
		"version = 1\n[[entry]]\nscope = \"dot\"\nkind = \"note\"\ntext = \"no dot\"\n",
		"version = 1\n[[entry]]\nscope = \"project\"\nkind = \"gossip\"\ntext = \"x\"\n",
		"version = 1\n[[entry\n",
	} {
		if _, err := ImportTOML([]byte(doc)); !errors.Is(err, ErrInvalid) {
			t.Errorf("ImportTOML(%q) err = %v, want ErrInvalid", doc, err)
		}
	}
	if _, err := ImportTOML([]byte("version = 2\n")); !errors.Is(err, ErrVersion) {
		t.Fatalf("version 2: %v", err)
	}
}

func TestSecretHitsAndUneditedSecretStillImports(t *testing.T) {
	entries := []Entry{
		{ID: "m1", Scope: ScopeProject, Kind: KindNote, Text: "use AKIAIOSFODNN7EXAMPLE for deploys", Status: StatusActive, Source: SourceUser},
		{ID: "m2", Scope: ScopeProject, Kind: KindNote, Text: "plain note", Status: StatusActive, Source: SourceUser},
	}
	data, err := ExportTOML(entries)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ImportTOML(data)
	if err != nil {
		t.Fatalf("secret-like text must still import: %v", err)
	}
	adds, updates, deletes := DiffTOML(entries, back)
	if len(adds)+len(updates)+len(deletes) != 0 {
		t.Fatalf("unedited round trip: %+v %+v %+v", adds, updates, deletes)
	}
	hits := SecretHits(back)
	if len(hits) != 1 || hits[0].String() != "p:m1" {
		t.Fatalf("hits %+v", hits)
	}
	if len(SecretHits(adds)) != 0 {
		t.Fatal("no hits expected in an empty diff")
	}
}

func TestImportValidatesStatusSourceDotAndRef(t *testing.T) {
	base := "version = 1\n[[entry]]\nscope = \"dot\"\nkind = \"note\"\ntext = \"x\"\n"
	for name, doc := range map[string]string{
		"status": base + "dot = \"security\"\nstatus = \"deleted\"\n",
		"source": base + "dot = \"security\"\nsource = \"robot\"\n",
		"dot":    base + "dot = \"../../x\"\n",
		"ref+5":  base + "dot = \"security\"\nref = \"d:m+5\"\n",
		"ref-1":  base + "dot = \"security\"\nref = \"d:m-1\"\n",
	} {
		if _, err := ImportTOML([]byte(doc)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	ok := base + "dot = \"security\"\nstatus = \"proposed\"\nsource = \"model\"\nref = \"d:m5\"\n"
	got, err := ImportTOML([]byte(ok))
	if err != nil || got[0].Source != SourceModel || got[0].ID != "m5" {
		t.Fatalf("valid doc: %+v %v", got, err)
	}
}

// An entry not stored yet (no ID) exports without a ref in every scope: a
// "p:", "d:" or "g:" ref would not import back (fuzz crasher 8a748f204ecc8c6f).
func TestExportTOMLWritesNoRefForAnUnsavedEntry(t *testing.T) {
	for _, sc := range []Scope{ScopeProject, ScopeDot, ScopeGlobal} {
		e := Entry{Scope: sc, Kind: KindNote, Text: "x", Status: StatusActive, Source: SourceUser}
		if sc != ScopeProject {
			e.Dot = "security"
		}
		data, err := ExportTOML([]Entry{e})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "ref =") {
			t.Errorf("%s: an unsaved entry exported a ref:\n%s", sc, data)
		}
		if back, err := ImportTOML(data); err != nil || len(back) != 1 || back[0].ID != "" || back[0].Scope != sc {
			t.Errorf("%s: re-import gave %+v, %v", sc, back, err)
		}
	}
}
