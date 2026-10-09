package migrate

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/history"
	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// writerTree writes every file Phases 4–9 added, with the writers that
// wrote them in 0.4–0.9. Phase 10 changes none of those writers, so what they
// produce today is what an existing install has on disk.
func writerTree(t *testing.T) (Roots, paths.Paths) {
	t.Helper()
	base := t.TempDir()
	p := paths.New(filepath.Join(base, "config"), filepath.Join(base, "state"), "/tmp/herdr-test.sock")
	r := RootsOf(p)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	const proj = "g-0123456789abcdef"

	cf := credentials.File{Path: p.CredentialsFile(), Env: func(string) string { return "" }}
	if err := cf.Set("anthropic", "sk-ant-api03-"+strings.Repeat("aB3d", 12)); err != nil {
		t.Fatalf("credentials.toml (Phase 4): %v", err)
	}
	if err := mcpinstall.SaveRecords(mcpinstall.RecordPath(p.ConfigDir), []mcpinstall.Record{{
		Client: mcpinstall.ClientClaude, Scope: "user", Command: []string{"/x/bin/herdr-dots", "mcp"}, At: now}}); err != nil {
		t.Fatalf("mcp-install.json (Phase 5): %v", err)
	}
	ps := patchstore.Open(p.PatchesDir())
	if err := ps.Put(patchstore.Meta{ID: "0123456789abcdef", Dot: "security", Pane: "w1:p1", Repo: "/repo", Status: patchstore.StatusReady}); err != nil {
		t.Fatalf("patches/<id>.json (Phase 7): %v", err)
	}
	d, err := state.OpenDaily(p.DailyFile())
	if err != nil {
		t.Fatalf("daily.json (Phase 8): %v", err)
	}
	if err := d.Add("security", "research", 0.01, now); err != nil {
		t.Fatalf("daily.json (Phase 8): %v", err)
	}
	if err := queue.OpenInbox(p.InboxFile()).Put(queue.Card{Project: proj, Dot: "security", DotName: "Security",
		Severity: "warn", Body: "Scheduled review: token logged.", Mode: "queue", Origin: "scheduled"}); err != nil {
		t.Fatalf("inbox.json (Phase 8): %v", err)
	}
	st := memory.Open(p.MemoryDir(), memory.DefaultLimits())
	if err := st.SeeDots(proj, []string{"security"}, now); err != nil {
		t.Fatalf("meta.json (Phase 8): %v", err)
	}
	if _, err := st.ClaimSchedule(proj, "security", now, now); err != nil {
		t.Fatalf("schedule.json (Phase 8): %v", err)
	}
	if err := history.Open(p.HistoryDir()).Append(history.Record{T: history.TypeRun, At: now, Dot: "security",
		Kind: history.KindReview, Outcome: state.OutcomePass}); err != nil {
		t.Fatalf("history (Phase 9): %v", err)
	}
	if err := (&projectdots.TrustStore{Path: p.TrustFile()}).Trust(proj, ".herdr-dots/dots/review.toml",
		"/repo/.herdr-dots/dots/review.toml", projectdots.Sum([]byte("name = \"Review\"\n")), nil); err != nil {
		t.Fatalf("trusted.json (Phase 9): %v", err)
	}
	return r, p
}

// Everything Phases 4–9 write is current in 1.0, and the JSONL history is
// not a kind at all.
func TestFilesWrittenByPhases4To9AreCurrent(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r, _ := writerTree(t)
	rep, err := Default().Run(r, Options{})
	if err != nil || !rep.Empty() || len(rep.Backups) != 0 {
		t.Fatalf("files written by Phases 4–9 must be current: %+v %v", rep, err)
	}
}

// Every Phase 4–9 file a newer herdr-dots wrote is kept read-only, and its
// own writer then fails with ErrNewerVersion instead of dropping what it
// doesn't know.
func TestNewerPhase4To9FilesStayReadOnly(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r, p := writerTree(t)
	inbox := `{"version":2,"next":1,"cards":[],"lanes":["a"]}`
	patch := `{"version":2,"id":"0123456789abcdef","dot":"security","pane":"w1:p1","repo":"/repo","files":0,"bytes":0,"status":"ready","created_at":"2026-10-08T12:00:00Z","updated_at":"2026-10-08T12:00:00Z"}`
	writeFile(t, p.InboxFile(), inbox)
	writeFile(t, filepath.Join(p.PatchesDir(), "0123456789abcdef.json"), patch)
	writeFile(t, mcpinstall.RecordPath(p.ConfigDir), `{"version":3,"installs":[]}`)
	rep, err := Default().Run(r, Options{})
	if err != nil || len(rep.ReadOnly) != 3 || len(rep.Migrated) != 0 {
		t.Fatalf("report %+v %v", rep, err)
	}
	err = queue.OpenInbox(p.InboxFile()).Put(queue.Card{Project: "g-0123456789abcdef", Dot: "security", Body: "x", Mode: "queue"})
	if !errors.Is(err, fsutil.ErrNewerVersion) || readFile(t, p.InboxFile()) != inbox {
		t.Fatalf("a newer inbox must not be rewritten: %v", err)
	}
	if _, err := patchstore.Open(p.PatchesDir()).Update("0123456789abcdef", func(m *patchstore.Meta) { m.Stat = "x" }); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("a newer patch record must not be rewritten: %v", err)
	}
	if err := mcpinstall.SaveRecords(mcpinstall.RecordPath(p.ConfigDir), nil); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("newer mcp-install.json must not be rewritten: %v", err)
	}
}

// credentials.toml holds keys: a broken one is reported without the parse
// error, which would quote the line (and the key) into engine.log.
func TestBrokenCredentialsAreReportedWithoutTheirText(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r, p := writerTree(t)
	key := "sk-ant-api03-" + strings.Repeat("Zq9x", 12)
	writeFile(t, p.CredentialsFile(), "[anthropic]\napi_key = \""+key+"\n")
	rep, _ := Default().Run(r, Options{})
	if len(rep.Corrupt) != 1 || strings.Contains(rep.Summary(), "Zq9x") || !strings.Contains(rep.Corrupt[0], "details withheld") {
		t.Fatalf("report %q", rep.Summary())
	}
}

// Formats herdr-dots reads from outside its directories check their own
// version: a newer pack or memory export is refused, never half-read.
func TestExternalFormatsRefuseNewerVersions(t *testing.T) {
	if len(External) == 0 || len(Appended) == 0 {
		t.Fatal("External and Appended document the formats Run does not cover")
	}
	if _, err := dotpack.Parse([]byte("[pack]\nformat = 2\n")); !errors.Is(err, dotpack.ErrFormat) {
		t.Fatalf("a format-2 pack: %v", err)
	}
	if _, err := memory.ImportTOML([]byte("version = 2\n")); !errors.Is(err, memory.ErrVersion) {
		t.Fatalf("a version-2 memory export: %v", err)
	}
	if _, err := dots.DecodeStrict([]byte("version = 2\nname = \"X\"\n")); err == nil {
		t.Fatal("a project dot with a key this version doesn't know must be refused")
	}
	if dotpack.Format != 1 {
		t.Fatalf("dotpack.Format = %d: register the change in External and CHANGELOG.md", dotpack.Format)
	}
}
