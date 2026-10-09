package dots

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid(name string) Dot {
	d := New(name)
	d.Purpose = "Line one.\nLine two."
	return d
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := valid("Security")
	d.Trigger.FileGlobs = []string{"**/*.ts"}
	if err := Save(dir, d, true); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "security")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Security" || got.Purpose != d.Purpose || got.Trigger.FileGlobs[0] != "**/*.ts" || got.Slug != "security" {
		t.Fatalf("round trip: %+v", got)
	}
	st, _ := os.Stat(filepath.Join(dir, "security.toml"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "security.toml"))
	if !strings.Contains(string(raw), `"""`) {
		t.Fatalf("purpose should be written as a multi-line string:\n%s", raw)
	}
}

func TestSaveCreateRefusesDuplicateSlug(t *testing.T) {
	dir := t.TempDir()
	_ = Save(dir, valid("Security"), true)
	err := Save(dir, valid("security!"), true)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if err := Save(dir, valid("Security"), false); err != nil {
		t.Fatalf("update must succeed: %v", err)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	if err := Save(t.TempDir(), New("x"), true); err == nil {
		t.Fatal("missing purpose must be rejected")
	}
}

func TestLoadAllSkipsInvalidAndReportsLine(t *testing.T) {
	dir := t.TempDir()
	_ = Save(dir, valid("Good"), true)
	os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("name = \"B\"\npurpose = = \"x\"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "empty.toml"), []byte("name = \"E\"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600)
	list, errs := LoadAll(dir)
	if len(list) != 1 || list[0].Slug != "good" {
		t.Fatalf("list = %+v", list)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %+v", errs)
	}
	var broken LoadError
	for _, e := range errs {
		if e.File == "broken.toml" {
			broken = e
		}
	}
	if broken.Line != 2 {
		t.Fatalf("broken line = %d (%v)", broken.Line, broken.Err)
	}
}

func TestLoadAllMissingDirIsEmpty(t *testing.T) {
	list, errs := LoadAll(filepath.Join(t.TempDir(), "nope"))
	if list != nil || errs != nil {
		t.Fatal("missing dir should be empty, not an error")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	_ = Save(dir, valid("Gone"), true)
	if err := Delete(dir, "gone"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(dir, "gone"); err == nil {
		t.Fatal("deleting twice should error")
	}
}
