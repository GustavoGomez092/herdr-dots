package projectdots

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/platform/platformtest"
)

const pdProject = "g-9f2c1a7b00112233"

// helperTOML is the review-focus case: a repo shipping an inject dot.
const helperTOML = `name = "Helper"
glyph = "h"
color = "cyan"
purpose = """
Be helpful."""

[delivery]
mode = "inject"
`

func pdRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func pdTrust(t *testing.T) *TrustStore {
	t.Helper()
	return &TrustStore{Path: filepath.Join(t.TempDir(), "config", "trusted.json")}
}

// pdWriteStore puts raw bytes in ts's file, failing the test if it can't.
func pdWriteStore(t *testing.T, ts *TrustStore, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(ts.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ts.Path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUntrustedFileIsClampedAndListsWhatItAsks(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	s := ScanProject(root, pdProject, pdTrust(t))
	if len(s.Items) != 1 || len(s.Invalid) != 0 {
		t.Fatalf("scan %+v", s)
	}
	it := s.Items[0]
	if it.Trusted || it.Dot.Slug != "proj--helper--9f2c1a7b" || it.Rel != ".herdr-dots/dots/helper.toml" {
		t.Fatalf("item %+v", it)
	}
	if !slices.Equal(it.Requested, []string{dots.CapInject, dots.CapMCP}) {
		t.Fatalf("requested %v", it.Requested)
	}
	if it.Effective.Delivery.Mode != dots.ModeQueue || it.Effective.Capabilities.MCP || it.Dot.Delivery.Mode != dots.ModeInject {
		t.Fatalf("effective %+v", it.Effective)
	}
	if _, ok := s.Find("proj--helper--9f2c1a7b"); !ok {
		t.Fatal("Find")
	}
}

func TestTrustWithoutCapabilitiesStaysClampedWithInjectRuns(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	if err := ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil); err != nil {
		t.Fatal(err)
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if !it.Trusted || len(it.Granted) != 0 || it.Effective.Delivery.Mode != dots.ModeQueue {
		t.Fatalf("trusted, no caps: %+v", it)
	}
	if err := ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapInject, dots.CapWrite}); err != nil {
		t.Fatal(err)
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if !slices.Equal(it.Granted, []string{dots.CapInject}) || it.Effective.Delivery.Mode != dots.ModeInject {
		t.Fatalf("a grant applies only to what the file asks for: %+v", it)
	}
	platformtest.AssertPrivateFile(t, ts.Path)
}

func TestAnyEditRevokesTrustAndChangesTheStamp(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapInject})
	before := Stamp(root, ts)
	// Same size, one byte different.
	edited := []byte(helperTOML)
	edited[len("name = \"")] = 'J'
	if err := os.WriteFile(it.Dot.SourcePath, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if Stamp(root, ts) == before {
		t.Fatal("a same-size edit must change the stamp")
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if it.Trusted || it.Effective.Delivery.Mode != dots.ModeQueue {
		t.Fatalf("an edited file must be untrusted again: %+v", it)
	}
}

func TestTrustIsSharedByClonesOfTheSameProjectOnly(t *testing.T) {
	a := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	b := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(a, pdProject, ts).Items[0]
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	if !ScanProject(b, pdProject, ts).Items[0].Trusted {
		t.Fatal("a worktree of the same project with the same bytes is trusted")
	}
	if ScanProject(b, "g-0000000011112222", ts).Items[0].Trusted {
		t.Fatal("another project is not")
	}
}

func TestRevokeAndList(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	if es, _ := ts.List(); len(es) != 1 || es[0].Path != it.Dot.SourcePath {
		t.Fatalf("list %+v", es)
	}
	if found, err := ts.Revoke(pdProject, it.Rel); !found || err != nil {
		t.Fatalf("revoke %v %v", found, err)
	}
	if ScanProject(root, pdProject, ts).Items[0].Trusted {
		t.Fatal("revoked")
	}
	if found, _ := ts.Revoke(pdProject, it.Rel); found {
		t.Fatal("nothing left to revoke")
	}
}

func TestCorruptTrustFileFailsClosedAndIsNeverOverwritten(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	pdWriteStore(t, ts, "{not json")
	s := ScanProject(root, pdProject, ts)
	if s.Items[0].Trusted || len(s.Invalid) != 1 {
		t.Fatalf("scan %+v", s)
	}
	it := s.Items[0]
	if err := ts.Trust(pdProject, it.Rel, "", it.Sum, nil); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("trust over a corrupt file: %v", err)
	}
	if data, _ := os.ReadFile(ts.Path); string(data) != "{not json" {
		t.Fatal("the corrupt file was overwritten")
	}
	if err := ts.Trust(pdProject, it.Rel, "", it.Sum, []string{"shell"}); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

// A repository dot asking for Phase 7 and 8 powers (write, research, web,
// a schedule, commit reviews) does nothing in the background until each one
// is granted for these exact bytes; a grant restores only itself.
func TestProactiveAndWriteFieldsStayOffUntilGranted(t *testing.T) {
	busy := helperTOML + `
[backend]
kind = "claude-cli"

[capabilities]
write = true

[research]
enabled = true
web = true

[schedule]
cron = "0 9 * * 1-5"
task = "Summarize open TODOs."

[trigger]
on_commit = true
`
	root := pdRepo(t, map[string]string{"busy.toml": busy})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	want := []string{dots.CapInject, dots.CapWrite, dots.CapMCP, dots.CapResearch, dots.CapWeb, dots.CapSchedule, dots.CapCommit}
	if !slices.Equal(it.Requested, want) {
		t.Fatalf("requested %v, want %v", it.Requested, want)
	}
	if it.Effective.Proactive() || it.Effective.Capabilities.Write || it.Effective.Research.Web {
		t.Fatalf("untrusted: %+v", it.Effective)
	}
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	if it = ScanProject(root, pdProject, ts).Items[0]; !it.Trusted || it.Effective.Proactive() || it.Effective.Capabilities.Write {
		t.Fatalf("trusted without grants: %+v", it.Effective)
	}
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapResearch})
	it = ScanProject(root, pdProject, ts).Items[0]
	e := it.Effective
	if !e.Research.Enabled || e.Research.Web || e.Schedule.Cron != "" || e.Trigger.OnCommit || e.Capabilities.Write {
		t.Fatalf("research granted alone: %+v", e)
	}
}

func TestInvalidFilesAreReportedNotLoaded(t *testing.T) {
	root := pdRepo(t, map[string]string{"bad.toml": "name = \"Bad\"\npurpose = \"p\"\n[backend]\nargs = [\"--dangerously-skip-permissions\"]\n", "ok.toml": helperTOML})
	s := ScanProject(root, pdProject, pdTrust(t))
	if len(s.Items) != 1 || len(s.Invalid) != 1 || s.Invalid[0].File != ".herdr-dots/dots/bad.toml" {
		t.Fatalf("scan %+v", s)
	}
}

// The hash covers every byte, not the parsed fields: an edit that changes
// no field (a comment) still makes the file untrusted.
func TestCommentOnlyEditRevokesTrust(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	if err := ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapInject}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(it.Dot.SourcePath, []byte(helperTOML+"# reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if it.Trusted || len(it.Granted) != 0 {
		t.Fatalf("a comment-only edit must revoke trust: %+v", it)
	}
	if it.Runnable() != nil {
		t.Fatal("an edited file must not be runnable")
	}
}

// Item.Dot and Item.Effective are previews; Runnable is the only way to get
// a dot to run, and it is nil unless this exact content is trusted.
func TestRunnableIsNilUnlessThisContentIsTrusted(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	if d := it.Runnable(); d != nil {
		t.Fatalf("untrusted: pending trust, never runnable: %+v", d)
	}
	if err := ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil); err != nil {
		t.Fatal(err)
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	d := it.Runnable()
	if d == nil || d.Slug != it.Dot.Slug || d.Delivery.Mode != dots.ModeQueue || d.Capabilities.MCP {
		t.Fatalf("a trusted item runs clamped: %+v", d)
	}

	if err := os.WriteFile(it.Dot.SourcePath, []byte(helperTOML+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := ScanProject(root, pdProject, ts).Items[0].Runnable(); d != nil {
		t.Fatalf("changed: never runnable: %+v", d)
	}
	if err := os.WriteFile(it.Dot.SourcePath, []byte(helperTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if ScanProject(root, pdProject, ts).Items[0].Runnable() == nil {
		t.Fatal("the trusted bytes are back")
	}
	pdWriteStore(t, ts, "{not json")
	if d := ScanProject(root, pdProject, ts).Items[0].Runnable(); d != nil {
		t.Fatalf("corrupt store: never runnable: %+v", d)
	}
}

func TestTrustNeedsAFullMemoryProjectID(t *testing.T) {
	ts := pdTrust(t)
	for _, id := range []string{"", "nope", "g-XYZ", "g-9f2c1a7b", "g-9F2C1A7B00112233", "x-9f2c1a7b00112233", "g-9f2c1a7b001122334", " g-9f2c1a7b00112233"} {
		if err := ts.Trust(id, ".herdr-dots/dots/helper.toml", "", Sum([]byte("x")), nil); err == nil {
			t.Fatalf("project id %q accepted", id)
		}
	}
	for _, id := range []string{"g-9f2c1a7b00112233", "p-0123456789abcdef"} {
		if err := ts.Trust(id, ".herdr-dots/dots/helper.toml", "", Sum([]byte("x")), nil); err != nil {
			t.Fatalf("project id %q: %v", id, err)
		}
	}
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	if s := ScanProject(root, "", ts); len(s.Items) != 0 || len(s.Invalid) != 1 {
		t.Fatalf("no project id, no items: %+v", s)
	}
}

func TestCorruptTrustFileIsKeptAsideAndChecksFailClosed(t *testing.T) {
	ts := pdTrust(t)
	pdWriteStore(t, ts, `{"version": 1, "entries": [`)
	if ok, caps, err := ts.Check(pdProject, "x", Sum(nil)); ok || caps != nil || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("check on a corrupt file: %v %v %v", ok, caps, err)
	}
	if _, err := ts.List(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("list: %v", err)
	}
	if _, err := ts.Revoke(pdProject, "x"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("revoke: %v", err)
	}
	aside, err := os.ReadFile(ts.Path + ".corrupt")
	if err != nil || string(aside) != `{"version": 1, "entries": [` {
		t.Fatalf("a copy is kept aside: %q %v", aside, err)
	}
	platformtest.AssertPrivateFile(t, ts.Path+".corrupt")
	if data, _ := os.ReadFile(ts.Path); string(data) != `{"version": 1, "entries": [` {
		t.Fatal("the corrupt file was changed")
	}
}

// A store a newer herdr-dots wrote isn't corrupt: it says to upgrade, trusts
// nothing, and is never overwritten or copied.
func TestNewerTrustFileAsksForAnUpgrade(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	const newer = `{"version": 2, "entries": []}`
	pdWriteStore(t, ts, newer)
	s := ScanProject(root, pdProject, ts)
	if s.Items[0].Trusted || len(s.Invalid) != 1 || !errors.Is(s.Invalid[0].Err, ErrNewer) {
		t.Fatalf("scan %+v", s)
	}
	if !strings.Contains(ErrNewer.Error(), "upgrade") || errors.Is(ErrNewer, ErrCorrupt) {
		t.Fatalf("ErrNewer %q", ErrNewer)
	}
	if err := ts.Trust(pdProject, s.Items[0].Rel, "", s.Items[0].Sum, nil); !errors.Is(err, ErrNewer) {
		t.Fatalf("trust over a newer file: %v", err)
	}
	if data, _ := os.ReadFile(ts.Path); string(data) != newer {
		t.Fatal("the newer file was changed")
	}
	if _, err := os.Stat(ts.Path + ".corrupt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a newer file is not corrupt")
	}
}

// The store is reported once per scan, even when no file decodes.
func TestCorruptStoreIsReportedWhenNoFileDecodes(t *testing.T) {
	root := pdRepo(t, map[string]string{"bad.toml": "name = \"Bad\"\nnot toml\n"})
	ts := pdTrust(t)
	pdWriteStore(t, ts, "{not json")
	s := ScanProject(root, pdProject, ts)
	if len(s.Items) != 0 || len(s.Invalid) != 2 {
		t.Fatalf("scan %+v", s)
	}
	if s.Invalid[1].File != "trusted.json" || !errors.Is(s.Invalid[1].Err, ErrCorrupt) {
		t.Fatalf("store error %+v", s.Invalid[1])
	}
}

// A scan's stamp is what Stamp reports for the same state, so a poll
// comparing the two never rescans for nothing.
func TestScanStampMatchesStamp(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML, "bad.toml": "x"})
	ts := pdTrust(t)
	for _, store := range []string{"", "{not json", `{"version": 1, "entries": []}`} {
		if store != "" {
			pdWriteStore(t, ts, store)
		}
		if s := ScanProject(root, pdProject, ts); s.Stamp != Stamp(root, ts) {
			t.Fatalf("store %q: scan stamp %q, Stamp %q", store, s.Stamp, Stamp(root, ts))
		}
	}
	if s := ScanProject(root, pdProject, nil); s.Stamp != Stamp(root, nil) {
		t.Fatal("no store")
	}
}

// pdHostileRepo writes n valid project dots of about 4 KB each.
func pdHostileRepo(t *testing.T, n int) string {
	t.Helper()
	body := helperTOML + strings.Repeat("# padding padding padding padding padding padding padding\n", 70)
	root := t.TempDir()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("d%04d.toml", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func pdAllocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A repository shipping thousands of dots can't exhaust memory or make
// every poll slow: a scan reads at most dots.MaxProjectFiles files and
// reports the rest as one error.
func TestAHostileRepoIsReadBounded(t *testing.T) {
	root := pdHostileRepo(t, 3000)
	ts := pdTrust(t)
	var s Scan
	scanBytes := pdAllocated(func() { s = ScanProject(root, pdProject, ts) })
	if len(s.Items) != dots.MaxProjectFiles || len(s.Invalid) != 1 {
		t.Fatalf("%d items, invalid %v", len(s.Items), s.Invalid)
	}
	if le := s.Invalid[0]; le.File != ".herdr-dots/dots" || !strings.Contains(le.Err.Error(), "2936 more files not read") {
		t.Fatalf("overflow error %v", le)
	}
	if s.Items[0].Rel != ".herdr-dots/dots/d0000.toml" || s.Items[63].Rel != ".herdr-dots/dots/d0063.toml" {
		t.Fatalf("the first files by name are read: %s .. %s", s.Items[0].Rel, s.Items[63].Rel)
	}
	// Reading all 3000 allocates well over 100 MB; 64 take a few.
	t.Logf("ScanProject allocated %d KB", scanBytes>>10)
	if scanBytes > 40<<20 {
		t.Fatalf("ScanProject allocated %d bytes", scanBytes)
	}
	stamp := ""
	if b := pdAllocated(func() { stamp = Stamp(root, ts) }); b > 12<<20 {
		t.Fatalf("Stamp allocated %d bytes", b)
	}
	if stamp != s.Stamp {
		t.Fatal("stamp")
	}
}

func TestTrustFailsWhenTheStoreIsLocked(t *testing.T) {
	ts := pdTrust(t)
	ts.LockTimeout = 10 * time.Millisecond
	if err := os.MkdirAll(filepath.Dir(ts.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.TryLock(ts.Path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if err := ts.Trust(pdProject, ".herdr-dots/dots/helper.toml", "", Sum(nil), nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("trust under someone else's lock: %v", err)
	}
	if _, err := os.Stat(ts.Path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("nothing is written without the lock")
	}
}

func TestTrustChangesTheStamp(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	s := ScanProject(root, pdProject, ts)
	_ = ts.Trust(pdProject, s.Items[0].Rel, "", s.Items[0].Sum, nil)
	if Stamp(root, ts) == s.Stamp {
		t.Fatal("trusting a file must change the stamp")
	}
}

// Errors never show an absolute path (a home directory): files are named
// from the repository root and the store as trusted.json.
func TestErrorsNameNoAbsolutePaths(t *testing.T) {
	ts := &TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	if err := os.Mkdir(ts.Path, 0o700); err != nil { // unreadable as a file
		t.Fatal(err)
	}
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML, "locked.toml": helperTOML})
	locked := filepath.Join(root, ".herdr-dots", "dots", "locked.toml")
	canDeny := runtime.GOOS != "windows" && os.Geteuid() != 0
	if canDeny {
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
	}
	s := ScanProject(root, pdProject, ts)
	want := 1
	if canDeny {
		want = 2
	}
	if len(s.Invalid) != want {
		t.Fatalf("invalid %v", s.Invalid)
	}
	for _, le := range s.Invalid {
		if msg := le.Error(); strings.Contains(msg, root) || strings.Contains(msg, filepath.Dir(ts.Path)) {
			t.Fatalf("absolute path in %q", msg)
		}
	}
	for _, it := range s.Items {
		if it.Trusted {
			t.Fatal("an unreadable store trusts nothing")
		}
	}
	err := ts.Trust(pdProject, ".herdr-dots/dots/helper.toml", "", Sum(nil), nil)
	if err == nil || strings.Contains(err.Error(), filepath.Dir(ts.Path)) {
		t.Fatalf("trust error %v", err)
	}
}
