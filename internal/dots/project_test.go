package dots

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const projID = "g-9f2c1a7b00112233"

func writeProjectDot(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const a11yTOML = `name = "A11y"
glyph = "♿"
color = "green"
purpose = """
Check accessibility."""

[delivery]
mode = "inject"
`

func TestProjectSlugsAreNamespacedAndFileSafe(t *testing.T) {
	s := ProjectSlug("a11y", projID)
	if s != "proj--a11y--9f2c1a7b" {
		t.Fatalf("slug %q", s)
	}
	base, tag, ok := SplitProjectSlug(s)
	if !ok || base != "a11y" || tag != "9f2c1a7b" || !IsProjectSlug(s) || DisplaySlug(s) != "proj:a11y" {
		t.Fatalf("split %q %q %v", base, tag, ok)
	}
	if Slugify("proj--a11y--9f2c1a7b") == s || IsProjectSlug(Slugify("Proj  A11y")) {
		t.Fatal("a user dot name must never produce a project slug")
	}
	if DisplaySlug("security") != "security" || ProjectTag("bogus") != "" || ProjectTag("g-XYZ12345abc") != "" {
		t.Fatal("non-project inputs")
	}
}

func TestLoadProjectNamespacesAndMarksOrigin(t *testing.T) {
	root := t.TempDir()
	p := writeProjectDot(t, root, "a11y.toml", a11yTOML)
	ds, bad := LoadProject(root, projID)
	if len(bad) != 0 || len(ds) != 1 {
		t.Fatalf("dots %+v bad %+v", ds, bad)
	}
	d := ds[0]
	if d.Slug != "proj--a11y--9f2c1a7b" || d.Origin != OriginProject || d.SourcePath != p || d.Delivery.Mode != ModeInject {
		t.Fatalf("dot %+v", d)
	}
	if none, bad := LoadProject(t.TempDir(), projID); none != nil || bad != nil {
		t.Fatal("a repo without .herdr-dots has no project dots and no errors")
	}
}

func TestLoadProjectRefusesUnknownKeysLinksBigFilesAndBadNames(t *testing.T) {
	root := t.TempDir()
	writeProjectDot(t, root, "future.toml", a11yTOML+"\n[capabilities]\nshell = true\n")
	writeProjectDot(t, root, "Bad_Name.toml", a11yTOML)
	writeProjectDot(t, root, "big.toml", a11yTOML+"# "+strings.Repeat("x", MaxProjectFileBytes)+"\n")
	target := writeProjectDot(t, root, "real.toml", a11yTOML)
	if err := os.Symlink(target, filepath.Join(root, ".herdr-dots", "dots", "link.toml")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	ds, bad := LoadProject(root, projID)
	if len(ds) != 1 || ds[0].Slug != "proj--real--9f2c1a7b" {
		t.Fatalf("only real.toml loads: %+v", ds)
	}
	msgs := map[string]string{}
	for _, le := range bad {
		msgs[le.File] = le.Err.Error()
	}
	for file, want := range map[string]string{
		".herdr-dots/dots/future.toml":   "unknown keys capabilities.shell",
		".herdr-dots/dots/Bad_Name.toml": "file name",
		".herdr-dots/dots/big.toml":      "cap",
		".herdr-dots/dots/link.toml":     "not a link",
	} {
		if !strings.Contains(msgs[file], want) {
			t.Errorf("%s: got %q, want it to mention %q", file, msgs[file], want)
		}
	}
}

func TestLoadProjectRefusesALinkedDotsDirectory(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	writeProjectDot(t, elsewhere, "a11y.toml", a11yTOML)
	if err := os.Symlink(filepath.Join(elsewhere, ".herdr-dots"), filepath.Join(root, ".herdr-dots")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	ds, bad := LoadProject(root, projID)
	if len(ds) != 0 || len(bad) != 1 || !strings.Contains(bad[0].Err.Error(), "directory") {
		t.Fatalf("dots %+v bad %+v", ds, bad)
	}
}

func TestValidateReservesProjectSlugsAndChecksOrigin(t *testing.T) {
	d := New("Spoof")
	d.Purpose = "p"
	d.Slug = "proj--a11y--9f2c1a7b"
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "reserved for project dots") {
		t.Fatalf("user dot with a project slug: %v", err)
	}
	d.Origin = OriginProject
	if err := d.Validate(); err != nil {
		t.Fatalf("project dot: %v", err)
	}
	d.Slug = "a11y"
	if err := d.Validate(); err == nil {
		t.Fatal("project origin needs a project slug")
	}
	d.Slug, d.Origin = "spoof", "elsewhere"
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("unknown origin: %v", err)
	}
}

func TestUserFilesKeepTemplateAndImportOriginsOnly(t *testing.T) {
	dir := t.TempDir()
	tpl, _ := FromTemplate("security")
	if tpl.Origin != OriginTemplate {
		t.Fatalf("template origin %q", tpl.Origin)
	}
	if err := Save(dir, tpl, true); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "security")
	if err != nil || got.Origin != OriginTemplate {
		t.Fatalf("template origin lost: %+v %v", got, err)
	}
	plain := New("Plain")
	plain.Purpose = "p"
	if err := Save(dir, plain, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "plain.toml"))
	if strings.Contains(string(raw), "origin") {
		t.Fatalf("a user origin is never written:\n%s", raw)
	}
	if got, _ := Load(dir, "plain"); got.Origin != OriginUser {
		t.Fatalf("plain origin %q", got.Origin)
	}
	os.WriteFile(filepath.Join(dir, "sneaky.toml"), []byte("origin = \"project\"\n"+strings.TrimPrefix(a11yTOML, "")), 0o600)
	if got, err := Load(dir, "sneaky"); err != nil || got.Origin != OriginUser {
		t.Fatalf("a user file can't claim to be a project dot: %+v %v", got, err)
	}
}

func TestEncodeKeepsPurposeReadableAndRoundTrips(t *testing.T) {
	d := New("Round")
	d.Purpose = "Line one with \"quotes\" and a \\ backslash.\n[not a table]\nends with a quote\""
	d.Origin = OriginImport
	data, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "purpose = \"\"\"\n") {
		t.Fatalf("purpose not multi-line:\n%s", data)
	}
	got, err := DecodeStrict(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != d.Purpose || got.Origin != OriginImport || got.Name != "Round" {
		t.Fatalf("round trip: %+v", got)
	}
}

// Origin is set by the loader: a project file can't call itself a user,
// template or imported dot.
func TestLoadProjectIgnoresTheOriginInTheFile(t *testing.T) {
	root := t.TempDir()
	writeProjectDot(t, root, "user.toml", "origin = \"user\"\n"+a11yTOML)
	writeProjectDot(t, root, "tpl.toml", "origin = \"template\"\n"+a11yTOML)
	ds, bad := LoadProject(root, projID)
	if len(bad) != 0 || len(ds) != 2 {
		t.Fatalf("dots %+v bad %+v", ds, bad)
	}
	for _, d := range ds {
		if d.Origin != OriginProject {
			t.Fatalf("%s: origin %q", d.Slug, d.Origin)
		}
	}
}

// Project dots are untrusted repo content: every Validate rule applies,
// including Phase 8's control-character, cron-floor and schedule project rules.
func TestLoadProjectRunsFullValidate(t *testing.T) {
	root := t.TempDir()
	writeProjectDot(t, root, "bell.toml", a11yTOML+"\n[schedule]\ncron = \"0 9 * * *\"\ntask = \"ring \\u0007 the bell\"\n")
	writeProjectDot(t, root, "often.toml", a11yTOML+"\n[schedule]\ncron = \"*/5 * * * *\"\ntask = \"t\"\n")
	writeProjectDot(t, root, "relative.toml", a11yTOML+"\n[schedule]\nprojects = [\"../elsewhere\"]\n")
	writeProjectDot(t, root, "topic.toml", a11yTOML+"\n[research]\ntopics = [\"two\\nlines\"]\n")
	writeProjectDot(t, root, "spoof.toml", "name = \"Council\"\npurpose = \"p\"\n")
	ds, bad := LoadProject(root, projID)
	if len(ds) != 0 {
		t.Fatalf("nothing loads: %+v", ds)
	}
	msgs := map[string]string{}
	for _, le := range bad {
		msgs[le.File] = le.Err.Error()
	}
	for file, want := range map[string]string{
		".herdr-dots/dots/bell.toml":     "control characters",
		".herdr-dots/dots/often.toml":    "15 minutes",
		".herdr-dots/dots/relative.toml": "absolute path",
		".herdr-dots/dots/topic.toml":    "one line",
		".herdr-dots/dots/spoof.toml":    "reserved for council",
	} {
		if !strings.Contains(msgs[file], want) {
			t.Errorf("%s: got %q, want it to mention %q", file, msgs[file], want)
		}
	}
}

func TestDecodeProjectNeedsAProjectID(t *testing.T) {
	if _, err := DecodeProject(ProjectFile{Base: "a11y", Rel: "x", Data: []byte(a11yTOML)}, "bogus"); err == nil {
		t.Fatal("a project id without a tag can't name project dots")
	}
}

// Encode is how a dot leaves this machine (packs): every field survives a
// strict decode, and Slug and SourcePath are never written.
func TestEncodeRoundTripsEveryField(t *testing.T) {
	d := riskyDot()
	d.Backend.Kind, d.Backend.Args, d.Backend.Model = KindOpenRouter, []string{}, "m"
	d.Backend.MaxTokens = 900
	d.Trigger.FileGlobs, d.Trigger.Cooldown = []string{"**/*.go"}, 30
	d.Research.Topics = []string{"go 1.26 changes"}
	d.Schedule.Task = "Line one.\nLine two with \"quotes\"."
	d.Schedule.Projects = []string{ScheduleAttached}
	d.Budget.MaxUSDPerSession = 1.5
	d.Origin = OriginImport
	d.SourcePath = "/somewhere/else.toml"
	data, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "somewhere") || strings.Contains(string(data), "risky") {
		t.Fatalf("slug or source path written:\n%s", data)
	}
	got, err := DecodeStrict(data)
	if err != nil {
		t.Fatal(err)
	}
	want := d
	want.Slug, want.SourcePath = "", ""
	got.Slug = ""
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v\n%s", got, want, data)
	}
}

// Encode never writes secrets: a dot whose text holds something shaped like a
// key is refused, and the error names the field but not the value.
func TestEncodeRefusesSecrets(t *testing.T) {
	key := "sk-ant-" + strings.Repeat("a1B2", 8)
	for field, set := range map[string]func(*Dot){
		"purpose":          func(d *Dot) { d.Purpose = "use " + key + " for calls" },
		"research.topics":  func(d *Dot) { d.Research.Topics = []string{"ok", "token " + key} },
		"schedule.task":    func(d *Dot) { d.Schedule.Task = "curl -H 'Authorization: Bearer " + strings.Repeat("Zx9", 8) + "'" },
		"name":             func(d *Dot) { d.Name = key },
		"backend.base_url": func(d *Dot) { d.Backend.BaseURL = "https://u:" + strings.Repeat("p4ss", 3) + "@example.invalid" },
		"backend.args":     func(d *Dot) { d.Backend.Args = []string{"--header", "x-api-key: " + strings.Repeat("k3y", 4)} },
	} {
		d := New("Leaky")
		d.Purpose = "p"
		set(&d)
		data, err := Encode(d)
		if err == nil {
			t.Errorf("%s: secret written:\n%s", field, data)
			continue
		}
		if !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), key) {
			t.Errorf("%s: error %q", field, err)
		}
	}
	plain := New("Plain")
	plain.Purpose = "Flag hard-coded credentials, API keys and tokens in code under review."
	if _, err := Encode(plain); err != nil {
		t.Fatalf("plain text is not a secret: %v", err)
	}
	for _, tpl := range Templates() {
		if _, err := Encode(tpl); err != nil {
			t.Errorf("template %s: %v", tpl.Slug, err)
		}
	}
}
