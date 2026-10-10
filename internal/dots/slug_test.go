package dots

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A slug reaches Load, Save and Delete from the manage popup and the CLI; it
// must never name a file outside the dots directory.
func TestSlugsThatAreNotFileNamesAreRefused(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "dots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim.toml")
	if err := os.WriteFile(victim, []byte("name = \"x\"\npurpose = \"p\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"../victim", "", "a/b", `..\victim`, "c:victim"} {
		if _, err := Load(dir, slug); !errors.Is(err, ErrBadSlug) {
			t.Errorf("Load(%q): %v", slug, err)
		}
		if err := Delete(dir, slug); !errors.Is(err, ErrBadSlug) {
			t.Errorf("Delete(%q): %v", slug, err)
		}
		if slug == "" {
			continue // Save makes a slug from the name
		}
		d := New("Fine")
		d.Purpose = "p"
		d.Slug = slug
		if err := Save(dir, d, false); !errors.Is(err, ErrBadSlug) {
			t.Errorf("Save with slug %q: %v", slug, err)
		}
	}
	// The error names the file it refused, readable on a status line.
	if _, err := Load(dir, "../victim"); err == nil || !strings.Contains(err.Error(), `"../victim.toml"`) {
		t.Errorf("the refusal names the file: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("a file outside the dots directory was touched")
	}
	d := New("Fine Dot")
	d.Purpose = "p"
	if err := Save(dir, d, true); err != nil {
		t.Fatalf("a normal dot must still save: %v", err)
	}
	if _, err := Load(dir, "fine-dot"); err != nil {
		t.Fatalf("and load: %v", err)
	}
	// A file the developer named by hand keeps loading by its own name.
	if err := os.WriteFile(filepath.Join(dir, "My_Dot.toml"), []byte("name = \"Mine\"\npurpose = \"p\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(dir, "My_Dot"); err != nil || got.Name != "Mine" {
		t.Fatalf("a hand-named dot file: %+v %v", got, err)
	}
}
