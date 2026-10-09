package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
)

const dotsTestPID = "g-9f2c1a7b00112233"

type dotsRig struct {
	c           *dotsCLI
	out, errOut *bytes.Buffer
	repo        string
	root        string
}

func newDotsCLI(t *testing.T) *dotsRig {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &dotsRig{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, repo: repo, root: root}
	r.c = &dotsCLI{
		dotsDir: filepath.Join(root, "config", "dots"),
		mem:     memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits()),
		hist:    history.Open(filepath.Join(root, "history")),
		trust:   &projectdots.TrustStore{Path: filepath.Join(root, "config", "trusted.json")},
		resolve: func(string) (memory.Project, error) {
			return memory.Project{ID: dotsTestPID, Name: "repo", Root: repo}, nil
		},
		fetch: func(context.Context, string) ([]byte, string, error) { return nil, "", os.ErrNotExist },
		cwd:   repo, out: r.out, errOut: r.errOut, in: strings.NewReader(""),
		now: func() time.Time { return time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC) },
	}
	return r
}

func (r *dotsRig) run(t *testing.T, args ...string) {
	t.Helper()
	if err := r.c.run(args); err != nil {
		t.Fatalf("dots %v: %v\nout:\n%s\nerr:\n%s", args, err, r.out, r.errOut)
	}
}

func (r *dotsRig) saveDot(t *testing.T, d dots.Dot) {
	t.Helper()
	if err := dots.Save(r.c.dotsDir, d, true); err != nil {
		t.Fatal(err)
	}
}

func TestDotsCommandIsRegistered(t *testing.T) {
	if commands["dots"] == nil {
		t.Fatal(`"dots" must be in the commands map`)
	}
}

func TestExportImportRoundTripNeverOverwrites(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Security")
	d.Purpose = "Review security."
	d.Delivery.Mode = dots.ModeInject
	r.saveDot(t, d)
	if _, err := r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Prefer zod.", Source: memory.SourceModel}); err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(r.repo, "security.dotpack.toml")
	r.run(t, "export", "security", "--with-memory", "dot", "-o", pack, "--note", "for the team")
	if err := r.c.run([]string{"export", "security", "-o", pack}); err == nil {
		t.Fatal("export over an existing file")
	}
	err := r.c.run([]string{"import", pack, "--yes"})
	if err == nil || !strings.Contains(err.Error(), "--as") {
		t.Fatalf("a slug collision must ask for --as: %v", err)
	}
	r.out.Reset()
	r.run(t, "import", pack, "--as", "Team Security", "--yes")
	o := r.out.String()
	for _, want := range []string{"IMPORT Team Security", "note: for the team", "Review security.", "inject: delivery.mode inject → queue", "--keep inject", "1. [dot convention] Prefer zod.", "imported Team Security as team-security"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
	got, err := dots.Load(r.c.dotsDir, "team-security")
	if err != nil || got.Origin != dots.OriginImport || got.Delivery.Mode != dots.ModeQueue || got.Capabilities.MCP {
		t.Fatalf("imported %+v %v", got, err)
	}
	es, _ := r.c.mem.List(dotsTestPID, memory.Filter{Scope: memory.ScopeDot, Dot: "team-security"})
	if len(es) != 1 || es[0].Source != memory.SourceImport || es[0].Status != memory.StatusProposed {
		t.Fatalf("memory %+v", es)
	}
	if orig, _ := dots.Load(r.c.dotsDir, "security"); orig.Delivery.Mode != dots.ModeInject {
		t.Fatal("the original dot changed")
	}
}

func TestImportNeedsYesWithoutATerminal(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Docs")
	d.Purpose = "Docs."
	r.saveDot(t, d)
	pack := filepath.Join(r.repo, "docs.dotpack.toml")
	r.run(t, "export", "docs", "-o", pack)
	err := r.c.run([]string{"import", pack, "--as", "Docs Two"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err %v", err)
	}
	if _, err := dots.Load(r.c.dotsDir, "docs-two"); err == nil {
		t.Fatal("imported without confirmation")
	}
	if err := r.c.run([]string{"import", pack, "--as", "Docs Two", "--keep", "bogus", "--yes"}); err == nil {
		t.Fatal("an unknown capability must be refused")
	}
	r.c.interactive, r.c.in = true, strings.NewReader("y\n")
	r.run(t, "import", pack, "--as", "Docs Two", "--keep", "mcp")
	if got, _ := dots.Load(r.c.dotsDir, "docs-two"); !got.Capabilities.MCP {
		t.Fatal("--keep mcp")
	}
}

func TestImportDefaultKeepsNothing(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Risky")
	d.Purpose = "Do risky things."
	d.Delivery.Mode = dots.ModeInject
	d.Capabilities.MCP = true
	d.Research.Enabled, d.Research.Web = true, true
	d.Trigger.OnCommit = true
	r.saveDot(t, d)
	r.run(t, "export", "risky")
	data := append([]byte(nil), r.out.Bytes()...)
	r.c.fetch = func(context.Context, string) ([]byte, string, error) { return data, "https://x.invalid/r.toml", nil }
	r.run(t, "import", "https://x.invalid/r", "--as", "Risky Copy", "--yes")
	got, err := dots.Load(r.c.dotsDir, "risky-copy")
	if err != nil {
		t.Fatal(err)
	}
	if got.Delivery.Mode == dots.ModeInject || got.Capabilities.MCP || got.Research.Enabled || got.Research.Web || got.Trigger.OnCommit {
		t.Fatalf("imported with a capability on: %+v", got)
	}
}

func TestImportPreviewShowsWhatReachesAPrompt(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Wide")
	d.Purpose = "Be wide."
	d.Backend.Model = "sonnet"
	d.Budget.MaxUSDPerSession = 50
	d.Budget.MaxCallsPerSession = 5000
	d.Research.Topics = []string{"upstream CVEs"}
	d.Schedule.Task = "Summarize the week."
	r.saveDot(t, d)
	r.run(t, "export", "wide", "--note", "careful now")
	data := append([]byte(nil), r.out.Bytes()...)
	r.c.fetch = func(context.Context, string) ([]byte, string, error) {
		return data, "https://user:pw@x.invalid/w.toml?token=abc", nil
	}
	r.out.Reset()
	err := r.c.run([]string{"import", "https://x.invalid/w", "--as", "Wide Two"})
	if err == nil {
		t.Fatal("no confirmation, no import")
	}
	o := r.out.String()
	for _, want := range []string{"claude-cli, model sonnet", "$2 per session", "research topics:", "upstream CVEs", "schedule task:", "Summarize the week.", "by herdr-dots", "note: careful now", "budget.max_usd_per_session 50 → 2"} {
		if !strings.Contains(o, want) {
			t.Errorf("preview missing %q:\n%s", want, o)
		}
	}
	for _, bad := range []string{"user:pw", "token=abc"} {
		if strings.Contains(o, bad) {
			t.Errorf("preview leaks %q:\n%s", bad, o)
		}
	}
	if _, err := dots.Load(r.c.dotsDir, "wide-two"); err == nil {
		t.Fatal("saved before confirming")
	}
}

func TestPreviewTextIsSanitized(t *testing.T) {
	r := newDotsCLI(t)
	// The preview helpers are given text a hostile pack could have slipped
	// past Parse in the future: they must still strip bidi and escapes.
	got := indentBlock("a\u202eb\x1b[31mred\nline2")
	if strings.ContainsAny(got, "\u202e\x1b") || !strings.Contains(got, "    line2") {
		t.Fatalf("indentBlock %q", got)
	}
	if got := r.c.shownSource("https://u:p@h.invalid/p?secret=1#frag"); strings.Contains(got, "secret") || strings.Contains(got, "u:p") || strings.Contains(got, "frag") {
		t.Fatalf("shownSource %q", got)
	}
}

func TestExportRefusesSecretMemoryUnlessForced(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Security")
	d.Purpose = "p"
	r.saveDot(t, d)
	r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", Source: memory.SourceModel})
	r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "Keep handlers small.", Source: memory.SourceModel})
	err := r.c.run([]string{"export", "security", "--with-memory", "dot"})
	if err == nil || !strings.Contains(err.Error(), "memory ") || strings.Contains(err.Error(), "ghp_") {
		t.Fatalf("err %v", err)
	}
	r.run(t, "export", "security", "--with-memory", "dot", "--force")
	if strings.Contains(r.out.String(), "ghp_") || !strings.Contains(r.out.String(), "Keep handlers small.") {
		t.Fatalf("forced export:\n%s", r.out)
	}
	e := r.errOut.String()
	if !strings.Contains(e, "left out 1 memory entry") || strings.Contains(e, "ghp_") {
		t.Fatalf("the drop must be reported by count, never by value:\n%s", e)
	}
}

func TestExportForceNeverDropsASecretInTheDot(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Leaky")
	d.Purpose = "Use ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789 to call the API."
	r.saveDot(t, d)
	err := r.c.run([]string{"export", "leaky", "--force"})
	if err == nil || strings.Contains(err.Error(), "ghp_") || !strings.Contains(err.Error(), "purpose") {
		t.Fatalf("err %v", err)
	}
}

func TestImportFromURLShowsTheFinalURL(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Docs")
	d.Purpose = "Docs."
	r.saveDot(t, d)
	r.run(t, "export", "docs")
	data := append([]byte(nil), r.out.Bytes()...)
	r.c.fetch = func(_ context.Context, url string) ([]byte, string, error) {
		return data, "https://raw.example.invalid/team/docs.dotpack.toml", nil
	}
	r.out.Reset()
	r.run(t, "import", "https://example.invalid/short", "--as", "Docs Web", "--yes")
	if !strings.Contains(r.out.String(), "from https://raw.example.invalid/team/docs.dotpack.toml") {
		t.Fatalf("final URL not shown:\n%s", r.out)
	}
}

func TestOutputPathsAreRelativeOrHomeAbbreviated(t *testing.T) {
	r := newDotsCLI(t)
	r.c.home = r.root
	d := dots.New("Docs")
	d.Purpose = "Docs."
	r.saveDot(t, d)
	pack := filepath.Join(r.root, "docs.dotpack.toml") // outside the repo, under "home"
	r.run(t, "export", "docs", "-o", pack)
	if got := r.errOut.String(); !strings.Contains(got, "to ~/docs.dotpack.toml") || strings.Contains(got, r.root) {
		t.Fatalf("export path:\n%s", got)
	}
	r.out.Reset()
	r.run(t, "import", pack, "--as", "Docs Two", "--yes")
	if got := r.out.String(); !strings.Contains(got, "from ~/docs.dotpack.toml") || strings.Contains(got, r.root) {
		t.Fatalf("import path:\n%s", got)
	}
}

func projectDotFile(t *testing.T, r *dotsRig) string {
	t.Helper()
	dir := filepath.Join(r.repo, ".herdr-dots", "dots")
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, "helper.toml")
	os.WriteFile(file, []byte("name = \"Helper\"\nglyph = \"h\"\npurpose = \"Be helpful.\"\n[delivery]\nmode = \"inject\"\n"), 0o644)
	return file
}

func TestTrustListAndUntrustAProjectDot(t *testing.T) {
	r := newDotsCLI(t)
	file := projectDotFile(t, r)
	r.run(t, "list")
	if !strings.Contains(r.out.String(), "proj:helper") || !strings.Contains(r.out.String(), "untrusted") {
		t.Fatalf("list:\n%s", r.out)
	}
	if err := r.c.run([]string{"trust", file, "--allow", "write", "--yes"}); err == nil || !strings.Contains(err.Error(), "doesn't ask for write") {
		t.Fatalf("granting what isn't asked: %v", err)
	}
	r.out.Reset()
	r.run(t, "trust", file, "--allow", "inject", "--yes")
	for _, want := range []string{"TRUST .herdr-dots/dots/helper.toml", "Be helpful.", "asks for: inject, mcp", "granting: inject", "stays off: mcp"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("trust preview missing %q:\n%s", want, r.out)
		}
	}
	data, _ := os.ReadFile(file)
	ok, caps, _ := r.c.trust.Check(dotsTestPID, ".herdr-dots/dots/helper.toml", projectdots.Sum(data))
	if !ok || strings.Join(caps, ",") != "inject" {
		t.Fatalf("trust %v %v", ok, caps)
	}
	r.run(t, "untrust", file)
	if ok, _, _ := r.c.trust.Check(dotsTestPID, ".herdr-dots/dots/helper.toml", projectdots.Sum(data)); ok {
		t.Fatal("still trusted")
	}
	if err := r.c.run([]string{"trust", filepath.Join(r.repo, "elsewhere.toml"), "--yes"}); err == nil {
		t.Fatal("a file outside .herdr-dots/dots")
	}
}

func TestTrustBindsTheShownSumAndNamesAnotherLocation(t *testing.T) {
	r := newDotsCLI(t)
	file := projectDotFile(t, r)
	data, _ := os.ReadFile(file)
	sum := projectdots.Sum(data)
	// Trusted from a different checkout of the same repository.
	other := filepath.Join(r.root, "other-clone", ".herdr-dots", "dots", "helper.toml")
	if err := r.c.trust.Trust(dotsTestPID, ".herdr-dots/dots/helper.toml", other, sum, nil); err != nil {
		t.Fatal(err)
	}
	r.c.home = r.root
	r.run(t, "list")
	if !strings.Contains(r.out.String(), "trusted at ~/other-clone/.herdr-dots/dots/helper.toml") {
		t.Fatalf("list:\n%s", r.out)
	}
	r.out.Reset()
	r.run(t, "trust", file, "--yes")
	o := r.out.String()
	if !strings.Contains(o, "sha256:  "+projectdots.Hex(sum)) || !strings.Contains(o, "already trusted at ~/other-clone/") {
		t.Fatalf("preview:\n%s", o)
	}
}

func TestTrustDeclinedOrChangedTrustsNothing(t *testing.T) {
	r := newDotsCLI(t)
	file := projectDotFile(t, r)
	data, _ := os.ReadFile(file)
	rel := ".herdr-dots/dots/helper.toml"
	r.c.interactive, r.c.in = true, strings.NewReader("n\n")
	if err := r.c.run([]string{"trust", file}); err == nil {
		t.Fatal("a no must not trust")
	}
	if ok, _, _ := r.c.trust.Check(dotsTestPID, rel, projectdots.Sum(data)); ok {
		t.Fatal("trusted after no")
	}
	// A file that changes between the preview and the answer is not trusted:
	// the answer comes from this reader, which edits the file as it is read.
	r.c.in = readerFunc(func(p []byte) (int, error) {
		os.WriteFile(file, append(data, []byte("\n# changed\n")...), 0o644)
		return copy(p, "y\n"), nil
	})
	err := r.c.run([]string{"trust", file})
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err %v", err)
	}
	now, _ := os.ReadFile(file)
	if ok, _, _ := r.c.trust.Check(dotsTestPID, rel, projectdots.Sum(now)); ok {
		t.Fatal("trusted bytes that were never shown")
	}
}

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestProfileShowsStatsMemoryHintsAndHistory(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Security")
	d.Purpose = "Review security."
	r.saveDot(t, d)
	now := r.c.now()
	for i := 11; i >= 0; i-- { // oldest first, as the engine appends
		card := history.CardKey("s", fmt.Sprint(i))
		at := now.Add(-time.Duration(i+1) * time.Hour)
		if err := r.c.hist.Append(history.Record{T: "run", At: at, Dot: "security", Kind: "review", Outcome: "flagged", Severity: "warn", Card: card, Excerpt: fmt.Sprintf("CSRF on /admin %d", i), CostUSD: 0.1, Ms: 4000}); err != nil {
			t.Fatal(err)
		}
		if err := r.c.hist.Append(history.Record{T: "resolve", At: at, Dot: "security", Card: card, Resolution: "dismissed"}); err != nil {
			t.Fatal(err)
		}
	}
	r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Prefer zod.", Source: memory.SourceModel})
	if _, err := r.c.mem.ImportPack(dotsTestPID, "security", []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Imported hint."}}); err != nil {
		t.Fatal(err)
	}
	r.run(t, "profile", "security", "--rows", "3")
	o := r.out.String()
	for _, want := range []string{"Security (security)", "LAST 7 DAYS", "LAST 30 DAYS", "runs 12", "accepted 0%", "MEMORY", "2 entries", "1 waiting for approval (1 imported)", "HINTS", "Mostly dismissed", "CSRF on /admin 0", "dismissed"} {
		if !strings.Contains(o, want) {
			t.Errorf("profile missing %q:\n%s", want, o)
		}
	}
	if strings.Contains(o, "CSRF on /admin 5") {
		t.Errorf("--rows 3 shows more than 3 rows:\n%s", o)
	}
	if err := r.c.run([]string{"profile", "nobody"}); err == nil {
		t.Fatal("unknown dot")
	}
}
