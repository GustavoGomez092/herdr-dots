package dots

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzDotFile loads arbitrary dot files: each is either reported with its
// file name or loads as a dot that validates and saves back.
func FuzzDotFile(f *testing.F) {
	for _, name := range []string{"security", "frontend", "test-writer"} {
		if b, err := templateFS.ReadFile("templates/" + name + ".toml"); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("name = \"X\"\n[delivery]\nmode = \"inject\"\n[budget]\nmax_calls_per_session = -1\n")
	f.Add("name = \"\"\n")
	f.Add("name = \"A\"\n[backend]\nargs = [\"--dangerously-skip-permissions\"]\n")
	f.Fuzz(func(t *testing.T, data string) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "fuzz.toml"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		ds, bad := LoadAll(dir)
		if len(ds)+len(bad) != 1 {
			t.Fatalf("one file gave %d dots and %d errors", len(ds), len(bad))
		}
		if len(bad) == 1 {
			if bad[0].File != "fuzz.toml" || bad[0].Err == nil {
				t.Fatalf("load error without its file: %+v", bad[0])
			}
			return
		}
		d := ds[0]
		if err := d.Validate(); err != nil {
			t.Fatalf("LoadAll returned an invalid dot: %v", err)
		}
		out := t.TempDir()
		if err := Save(out, d, false); err != nil {
			t.Fatalf("a loaded dot must save: %v", err)
		}
		back, err := Load(out, d.Slug)
		if err != nil || back.Name != d.Name || back.Purpose != d.Purpose {
			t.Fatalf("save/load changed the dot: %+v vs %+v (%v)", back, d, err)
		}
	})
}

// FuzzDecodeStrict feeds arbitrary bytes to the strict decoder that reads
// repository project dots (files other people commit). A file it accepts
// never claims an origin of its own, and DecodeProject turns it into a
// project dot under a project-namespaced slug that validates.
func FuzzDecodeStrict(f *testing.F) {
	for _, name := range []string{"security", "frontend", "backend"} {
		if b, err := templateFS.ReadFile("templates/" + name + ".toml"); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("name = \"X\"\npurpose = \"p\"\norigin = \"user\"\n")
	f.Add("name = \"X\"\npurpose = \"p\"\n[capabilities]\nwrite = true\nfuture = 1\n")
	f.Add("name = \"X\"\npurpose = \"p\"\n[delivery]\nmode = \"inject\"\n")
	f.Add("version = 2\n")
	f.Fuzz(func(t *testing.T, data string) {
		d, err := DecodeStrict([]byte(data))
		if err != nil {
			return
		}
		if d.Origin != "" {
			t.Fatalf("DecodeStrict trusted the file's origin %q", d.Origin)
		}
		pf := ProjectFile{Base: "review", Path: "/repo/.herdr-dots/dots/review.toml", Rel: ".herdr-dots/dots/review.toml", Data: []byte(data)}
		pd, err := DecodeProject(pf, "g-0123456789abcdef")
		if err != nil {
			return
		}
		if pd.Origin != OriginProject || !IsProjectSlug(pd.Slug) || pd.SourcePath != pf.Path {
			t.Fatalf("project dot %q: origin %q, source %q", pd.Slug, pd.Origin, pd.SourcePath)
		}
		if err := pd.Validate(); err != nil {
			t.Fatalf("DecodeProject returned an invalid dot: %v", err)
		}
	})
}
