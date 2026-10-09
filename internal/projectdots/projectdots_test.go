package projectdots

import (
	"errors"
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
	_ = os.MkdirAll(filepath.Dir(ts.Path), 0o700)
	os.WriteFile(ts.Path, []byte("{not json"), 0o600)
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
	if _, ok := it.Runnable(); ok {
		t.Fatal("an edited file must not be runnable")
	}
}

// Only a trusted item is runnable, and what runs is the clamped copy.
func TestOnlyTrustedItemsAreRunnable(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	if d, ok := it.Runnable(); ok || d.Slug != "" {
		t.Fatalf("an untrusted item is pending trust, never runnable: %+v %v", d, ok)
	}
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	it = ScanProject(root, pdProject, ts).Items[0]
	d, ok := it.Runnable()
	if !ok || d.Slug != it.Dot.Slug || d.Delivery.Mode != dots.ModeQueue || d.Capabilities.MCP {
		t.Fatalf("a trusted item runs clamped: %+v %v", d, ok)
	}
}

func TestTrustRefusesAProjectIDThatCantNameProjectDots(t *testing.T) {
	ts := pdTrust(t)
	for _, id := range []string{"", "nope", "g-XYZ"} {
		if err := ts.Trust(id, ".herdr-dots/dots/helper.toml", "", Sum([]byte("x")), nil); err == nil {
			t.Fatalf("project id %q accepted", id)
		}
	}
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	if s := ScanProject(root, "", ts); len(s.Items) != 0 || len(s.Invalid) != 1 {
		t.Fatalf("no project id, no items: %+v", s)
	}
}

func TestCorruptTrustFileIsKeptAsideAndChecksFailClosed(t *testing.T) {
	ts := pdTrust(t)
	_ = os.MkdirAll(filepath.Dir(ts.Path), 0o700)
	os.WriteFile(ts.Path, []byte(`{"version": 99, "entries": []}`), 0o600)
	if ok, caps, err := ts.Check(pdProject, "x", Sum(nil)); ok || caps != nil || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("check on a newer file: %v %v %v", ok, caps, err)
	}
	if _, err := ts.List(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("list: %v", err)
	}
	if _, err := ts.Revoke(pdProject, "x"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("revoke: %v", err)
	}
	aside, err := os.ReadFile(ts.Path + ".corrupt")
	if err != nil || string(aside) != `{"version": 99, "entries": []}` {
		t.Fatalf("a copy is kept aside: %q %v", aside, err)
	}
	platformtest.AssertPrivateFile(t, ts.Path+".corrupt")
	if data, _ := os.ReadFile(ts.Path); string(data) != `{"version": 99, "entries": []}` {
		t.Fatal("the corrupt file was changed")
	}
}

func TestTrustFailsWhenTheStoreIsLocked(t *testing.T) {
	ts := pdTrust(t)
	ts.LockTimeout = 10 * time.Millisecond
	_ = os.MkdirAll(filepath.Dir(ts.Path), 0o700)
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
