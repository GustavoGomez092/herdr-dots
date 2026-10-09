package memory

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/platform/platformtest"
)

const pid = "g-0123456789abcdef"

func testStore(t *testing.T) *Store {
	t.Helper()
	lim := DefaultLimits()
	lim.LockTimeout = 5 * time.Second
	return Open(filepath.Join(t.TempDir(), "memory"), lim)
}

func dotNote(dot, text string) Entry {
	return Entry{Scope: ScopeDot, Dot: dot, Kind: KindNote, Text: text, Source: SourceModel}
}

func mustAdd(t *testing.T, s *Store, p string, e Entry) Entry {
	t.Helper()
	got, err := s.Add(p, e)
	if err != nil {
		t.Fatalf("Add(%+v): %v", e, err)
	}
	return got
}

func list(t *testing.T, s *Store, p string, f Filter) []Entry {
	t.Helper()
	es, err := s.List(p, f)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return es
}

func TestAddGetUpdateForgetRoundTrip(t *testing.T) {
	s := testStore(t)
	e := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindDecision, Text: " Auth tokens live in httpOnly cookies. ", Paths: []string{"web/**", "web/**", "[bad"}})
	if e.ID != "m1" || e.Text != "Auth tokens live in httpOnly cookies." || e.Status != StatusActive || e.Source != SourceUser || len(e.Paths) != 1 || e.CreatedAt.IsZero() {
		t.Fatalf("stored %+v", e)
	}
	got, err := s.Get(pid, e.Ref())
	if err != nil || got.Text != e.Text {
		t.Fatalf("Get: %+v %v", got, err)
	}
	up, err := s.Update(pid, e.Ref(), func(x *Entry) error { x.Pinned, x.Text = true, "Tokens live in httpOnly cookies."; return nil })
	if err != nil || !up.Pinned || up.Text != "Tokens live in httpOnly cookies." || up.ID != "m1" {
		t.Fatalf("Update: %+v %v", up, err)
	}
	if err := s.Forget(pid, e.Ref(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(pid, e.Ref()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after forget: %v", err)
	}
	if _, err := s.Update(pid, e.Ref(), func(*Entry) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestIDsAreNeverReused(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "first fact about tokens"))
	b := mustAdd(t, s, pid, dotNote("security", "second fact about cookies"))
	if err := s.Forget(pid, b.Ref(), false); err != nil {
		t.Fatal(err)
	}
	c := mustAdd(t, s, pid, dotNote("security", "third fact about headers"))
	if c.ID != "m3" {
		t.Fatalf("id %s, want m3", c.ID)
	}
	other := mustAdd(t, s, pid, dotNote("frontend", "frontend fact"))
	if other.ID != "m1" {
		t.Fatalf("ids are per file: %s", other.ID)
	}
}

func TestExactDuplicateBumpsUses(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "Rate limiting added to /login."))
	again := mustAdd(t, s, pid, dotNote("security", "rate LIMITING added to login"))
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 1 || again.Uses != 1 || es[0].Uses != 1 {
		t.Fatalf("entries %+v", es)
	}
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindDismissed, Text: "Rate limiting added to /login."})
	if n := len(list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})); n != 2 {
		t.Fatalf("dedupe is per kind; got %d entries", n)
	}
}

func TestNearDuplicateUpdatesWording(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "JWT stored in localStorage is readable by any XSS payload; move it to an httpOnly cookie."))
	mustAdd(t, s, pid, dotNote("security", "The JWT is stored in localStorage, which any XSS payload can read; move it to an httpOnly cookie."))
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 1 || !strings.HasPrefix(es[0].Text, "The JWT is stored") || es[0].Uses != 1 {
		t.Fatalf("entries %+v", es)
	}
}

func TestModelNeverRewritesDeveloperWording(t *testing.T) {
	s := testStore(t)
	mine := "JWT stored in localStorage is readable by any XSS payload; move it to an httpOnly cookie."
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: mine, Source: SourceUser})
	mustAdd(t, s, pid, dotNote("security", "The JWT is stored in localStorage, which any XSS payload can read; move it to an httpOnly cookie."))
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 1 || es[0].Text != mine || es[0].Source != SourceUser {
		t.Fatalf("entries %+v", es)
	}
}

// Follow-up to final review item 1: a sent card that merges into a dot's own
// model-written finding makes it an outcome finding (with the card's
// evidence), so it suppresses repeats. A user entry is never demoted and a
// model add never promotes anything.
func TestOutcomeMergePromotesModelFinding(t *testing.T) {
	const jwt = "JWT stored in localStorage is readable by any XSS payload; move it to an httpOnly cookie."
	modelEv := Evidence{Session: "s1", Pane: "w1:p1", Turn: 1}
	sentEv := Evidence{Session: "s1", Pane: "w1:p1", Turn: 1, Card: "7"}
	finding := func(src Source, ev Evidence) Entry {
		return Entry{Scope: ScopeDot, Dot: "security", Kind: KindFinding, Text: jwt, Source: src, Evidence: ev}
	}

	s := testStore(t)
	mustAdd(t, s, pid, finding(SourceModel, modelEv))
	mustAdd(t, s, pid, finding(SourceOutcome, sentEv))
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 1 || es[0].Source != SourceOutcome || es[0].Evidence != sentEv {
		t.Fatalf("an outcome merge must promote model→outcome with the card's evidence: %+v", es)
	}

	s = testStore(t)
	userEv := Evidence{Pane: "w1:p1", Turn: 1, Card: "3"}
	mustAdd(t, s, pid, finding(SourceUser, userEv))
	mustAdd(t, s, pid, finding(SourceOutcome, sentEv))
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 1 || es[0].Source != SourceUser || es[0].Evidence != userEv {
		t.Fatalf("a user entry stays the developer's: %+v", es)
	}

	s = testStore(t)
	mustAdd(t, s, pid, finding(SourceOutcome, sentEv))
	mustAdd(t, s, pid, finding(SourceModel, Evidence{Session: "s1", Turn: 9}))
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 1 || es[0].Source != SourceOutcome || es[0].Evidence != sentEv {
		t.Fatalf("a model add must not change an outcome entry: %+v", es)
	}
}

func TestCapArchivesLowestValueButNeverPinnedOrUser(t *testing.T) {
	s := testStore(t)
	s.Limits.MaxDot = 4
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "developer note alpha", Source: SourceUser})
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "pinned model note bravo", Source: SourceModel, Pinned: true})
	low := mustAdd(t, s, pid, dotNote("security", "unused model note charlie"))
	used := mustAdd(t, s, pid, dotNote("security", "popular model note delta"))
	for i := 0; i < 3; i++ {
		if err := s.Touch(pid, []Ref{used.Ref()}); err != nil {
			t.Fatal(err)
		}
	}
	mustAdd(t, s, pid, dotNote("security", "newest model note echo"))
	all := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusActive, StatusArchived}})
	var archived, live []string
	for _, e := range all {
		if e.Status == StatusArchived {
			archived = append(archived, e.ID)
		} else {
			live = append(live, e.ID)
		}
	}
	if strings.Join(archived, ",") != low.ID || strings.Join(live, ",") != "m1,m2,m4,m5" {
		t.Fatalf("archived %v live %v; want only the unused model entry archived", archived, live)
	}
	for i := 0; i < 5; i++ {
		mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: fmt.Sprintf("developer rule number%d", i), Source: SourceUser})
	}
	for _, e := range list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusArchived}}) {
		if e.Source == SourceUser || e.Pinned {
			t.Fatalf("archived a developer or pinned entry: %+v", e)
		}
	}
}

func TestTextLimits(t *testing.T) {
	s := testStore(t)
	if _, err := s.Add(pid, dotNote("security", strings.Repeat("é", 501))); !errors.Is(err, ErrTooLong) {
		t.Fatalf("501 runes: %v", err)
	}
	if _, err := s.Add(pid, dotNote("security", strings.Repeat("é", 500))); err != nil {
		t.Fatalf("500 runes is allowed: %v", err)
	}
	if _, err := s.Add(pid, dotNote("security", "  ")); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := s.Add(pid, Entry{Scope: ScopeDot, Dot: "security", Kind: "rumor", Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad kind: %v", err)
	}
	if _, err := s.Add(pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "x", Status: StatusProposed}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("only project memory can be proposed: %v", err)
	}
}

// Entry text reaches terminals (memory browser, CLI) and prompts, like model
// text in v1's feed: control characters are stripped on write and on read
// (a hand edit or an old file), and paths holding them are dropped.
func TestControlCharactersAreStripped(t *testing.T) {
	s := testStore(t)
	e := mustAdd(t, s, pid, dotNote("security", "use zod\x1b]52;c;ZXZpbA==\x07 schemas\u009b2J\tnow"))
	if e.Text != "use zod]52;c;ZXZpbA== schemas2J\tnow" {
		t.Fatalf("stored %q", e.Text)
	}
	path := filepath.Join(s.Dir, pid, "dots", "frontend.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{"version":1,"entries":[{"id":"m1","kind":"note","status":"active","text":"hand\u001b[2J edit","paths":["web/\u001b[1m**","web/**"]}]}`), 0o600)
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "frontend"})
	if len(es) != 1 || es[0].Text != "hand[2J edit" || len(es[0].Paths) != 1 || es[0].Paths[0] != "web/**" {
		t.Fatalf("hand-edited entry: %+v", es)
	}
}

func TestNewerVersionIsReadOnly(t *testing.T) {
	s := testStore(t)
	path := filepath.Join(s.Dir, pid, "dots", "security.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	orig := []byte(`{"version":2,"entries":[{"id":"m1","kind":"note","text":"from the future"}]}`)
	os.WriteFile(path, orig, 0o600)
	if _, err := s.List(pid, Filter{Scope: ScopeDot, Dot: "security"}); !errors.Is(err, ErrVersion) {
		t.Fatalf("List: %v", err)
	}
	if _, err := s.Add(pid, dotNote("security", "new fact")); !errors.Is(err, ErrVersion) {
		t.Fatalf("Add: %v", err)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, orig) {
		t.Fatal("a newer file must never be overwritten")
	}
}

// Spec §8.1: a hand-broken file fails only its scope and is set aside only on
// the next write.
func TestCorruptFileFailsOnlyItsScopeAndIsSetAsideOnWrite(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindConvention, Text: "Components live in src/ui."})
	path := filepath.Join(s.Dir, pid, "dots", "security.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{oops"), 0o600)
	es, err := s.List(pid, Filter{})
	var se *ScopeError
	if !errors.Is(err, ErrCorrupt) || !errors.As(err, &se) || se.Dot != "security" || !strings.Contains(err.Error(), "dot (security) memory unreadable") {
		t.Fatalf("err %v", err)
	}
	if len(es) != 1 || es[0].Kind != KindConvention {
		t.Fatalf("other scopes keep working: %+v", es)
	}
	if b, _ := os.ReadFile(path); string(b) != "{oops" {
		t.Fatal("reading must never touch a corrupt file")
	}
	mustAdd(t, s, pid, dotNote("security", "fresh start"))
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt file not set aside: %v", matches)
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "{oops" {
		t.Fatal("the set-aside file must keep the original bytes")
	}
	if n := len(list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})); n != 1 {
		t.Fatalf("%d entries after recovery", n)
	}
}

func TestFilesArePrivate(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "a fact"))
	mustAdd(t, s, "", Entry{Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: "British spelling"})
	for _, d := range []string{s.Dir, filepath.Join(s.Dir, pid), filepath.Join(s.Dir, pid, "dots"), filepath.Join(s.Dir, GlobalID, "dots")} {
		platformtest.AssertPrivateDir(t, d)
	}
	for _, f := range []string{filepath.Join(s.Dir, pid, "dots", "security.json"), filepath.Join(s.Dir, pid, "memory.lock"), filepath.Join(s.Dir, GlobalID, "dots", "security.json")} {
		platformtest.AssertPrivateFile(t, f)
	}
}

// Review Focus 2: ids and slugs become paths; nothing may escape the memory dir.
func TestUnsafeIDsAndSlugsAreRefused(t *testing.T) {
	s := testStore(t)
	parent := filepath.Dir(s.Dir)
	checks := map[string]error{}
	_, checks["add ../ project"] = s.Add("../../etc", Entry{Scope: ScopeProject, Kind: KindNote, Text: "x"})
	_, checks["add ../ dot"] = s.Add(pid, Entry{Scope: ScopeDot, Dot: "../x", Kind: KindNote, Text: "x"})
	_, checks["add global ../"] = s.Add("", Entry{Scope: ScopeGlobal, Dot: "../../x", Kind: KindNote, Text: "x"})
	_, checks["list bad id"] = s.List("g-zz", Filter{})
	_, checks["list bad dot"] = s.List(pid, Filter{Dot: "a/b"})
	checks["purge .."] = s.Purge("..")
	_, checks["delete dot ../"] = s.DeleteDot("../x")
	checks["see project"] = s.SeeProject(Project{ID: "../x"})
	_, checks["forget bad"] = s.ForgetAll("p-../../../", ScopeProject, "")
	for name, err := range checks {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err %v, want ErrInvalid", name, err)
		}
	}
	ents, _ := os.ReadDir(parent)
	for _, e := range ents {
		if e.Name() != filepath.Base(s.Dir) {
			t.Errorf("created %s outside the memory dir", e.Name())
		}
	}
}

func TestForgetAllDeleteDotAndPurge(t *testing.T) {
	s := testStore(t)
	other := "p-00000000000000ff"
	mustAdd(t, s, pid, dotNote("security", "one fact"))
	mustAdd(t, s, pid, dotNote("security", "two facts"))
	mustAdd(t, s, other, dotNote("security", "other project fact"))
	mustAdd(t, s, "", Entry{Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: "terse please"})
	mustAdd(t, s, pid, dotNote("frontend", "frontend fact"))
	if n, err := s.ForgetAll(pid, ScopeDot, "security"); err != nil || n != 2 {
		t.Fatalf("ForgetAll: %d %v", n, err)
	}
	mustAdd(t, s, pid, dotNote("security", "back again"))
	if n, err := s.DeleteDot("security"); err != nil || n != 3 {
		t.Fatalf("DeleteDot: %d %v", n, err)
	}
	if es, _ := s.List("", Filter{Scope: ScopeGlobal, Dot: "security"}); len(es) != 0 {
		t.Fatal("global memory survived DeleteDot")
	}
	if n := len(list(t, s, pid, Filter{Scope: ScopeDot, Dot: "frontend"})); n != 1 {
		t.Fatal("DeleteDot touched another dot")
	}
	if err := s.Purge(pid); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, pid)); !os.IsNotExist(err) {
		t.Fatal("purge must remove the project directory")
	}
	if err := s.Purge(pid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second purge: %v", err)
	}
}

func TestSeeProjectProjectsAndFind(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	if err := s.SeeProject(Project{ID: pid, Name: "herdr-dots", Root: realPath(root)}); err != nil {
		t.Fatal(err)
	}
	_ = s.SeeProject(Project{ID: pid, Name: "herdr-dots", Root: "/elsewhere/herdr-dots"})
	_ = s.SeeProject(Project{ID: pid, Name: "herdr-dots", Root: realPath(root)})
	ps, err := s.Projects()
	if err != nil || len(ps) != 1 || ps[0].Name != "herdr-dots" || len(ps[0].Roots) != 2 || ps[0].CreatedAt.IsZero() {
		t.Fatalf("%+v %v", ps, err)
	}
	sub := filepath.Join(root, "internal")
	os.MkdirAll(sub, 0o700)
	if m, ok, _ := s.FindProject(sub); !ok || m.ID != pid {
		t.Fatalf("by path: %+v %v", m, ok)
	}
	if m, ok, _ := s.FindProject(pid); !ok || m.Name != "herdr-dots" {
		t.Fatalf("by id: %+v %v", m, ok)
	}
	if _, ok, _ := s.FindProject(t.TempDir()); ok {
		t.Fatal("unrelated path matched")
	}
}

func TestStatsAndCandidates(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindDecision, Text: "Tokens in httpOnly cookies.", Source: SourceUser})
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindConvention, Text: "Use zod for input validation.", Source: SourceModel, Status: StatusProposed})
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "frontend", Kind: KindConvention, Text: "Components live in src/ui.", Source: SourceModel, Status: StatusProposed})
	mustAdd(t, s, pid, dotNote("security", "security fact"))
	gone := mustAdd(t, s, pid, dotNote("security", "archived fact"))
	s.Update(pid, gone.Ref(), func(e *Entry) error { e.Status = StatusArchived; return nil })
	mustAdd(t, s, pid, dotNote("frontend", "frontend fact"))
	mustAdd(t, s, "", Entry{Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: "Be terse."})
	st, err := s.Stats(pid)
	if err != nil || st.Entries[""] != 3 || st.Entries["security"] != 1 || st.Entries["frontend"] != 1 || st.Proposals != 2 || st.Total() != 5 {
		t.Fatalf("%+v %v", st, err)
	}
	cs, err := s.Candidates(pid, "security", true)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, e := range cs {
		texts = append(texts, e.Text)
	}
	got := strings.Join(texts, "|")
	if got != "security fact|Tokens in httpOnly cookies.|Use zod for input validation.|Be terse." {
		t.Fatalf("candidates %q", got)
	}
	if cs, _ := s.Candidates(pid, "security", false); len(cs) != 2 {
		t.Fatalf("read_project=false must skip project memory: %+v", cs)
	}
}

func TestLockTimeoutReturnsErrLocked(t *testing.T) {
	s := testStore(t)
	s.Limits.LockTimeout = 100 * time.Millisecond
	dir := filepath.Join(s.Dir, pid)
	os.MkdirAll(dir, 0o700)
	held, err := lockfile.TryLock(filepath.Join(dir, "memory.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	start := time.Now()
	if _, err := s.Add(pid, dotNote("security", "blocked")); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if _, err := s.List(pid, Filter{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("reads take the lock too: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("waited %v", time.Since(start))
	}
}

func TestForgetScrubRemovesLedgerLines(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.Dir, pid)
	for _, o := range []Outcome{
		{Dot: "security", Card: "7", Kind: OutcomeDismissed, Body: "CSRF tokens on /admin are intentionally omitted."},
		{Dot: "security", Card: "8", Kind: OutcomeSent, Body: "Add rate limiting to /login."},
		{Dot: "frontend", Card: "7", Kind: OutcomeDismissed, Body: "Unrelated frontend card."},
	} {
		if err := s.withLock(dir, func() error { return s.appendLedgerLocked(pid, o) }); err != nil {
			t.Fatal(err)
		}
	}
	e := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindDismissed, Text: "CSRF tokens on /admin are intentionally omitted.", Source: SourceOutcome, Evidence: Evidence{Card: "7"}})
	if err := s.Forget(pid, e.Ref(), true); err != nil {
		t.Fatal(err)
	}
	led, err := s.Ledger(pid)
	if err != nil || len(led) != 2 || led[0].Card != "8" || led[1].Dot != "frontend" {
		t.Fatalf("ledger %+v %v", led, err)
	}
}

func TestConcurrentStoresLoseNoUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	lim := DefaultLimits()
	lim.MaxDot, lim.LockTimeout = 100000, 30*time.Second
	stores := []*Store{Open(dir, lim), Open(dir, lim)}
	const workers, each = 6, 20
	var wg sync.WaitGroup
	errs := make(chan error, len(stores)*workers*each)
	for si, s := range stores {
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < each; i++ {
					text := fmt.Sprintf("store%d worker%d item%d unique%05d", si, w, i, si*10000+w*100+i)
					if _, err := s.Add(pid, dotNote("security", text)); err != nil {
						errs <- err
					}
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	es := list(t, stores[0], pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != len(stores)*workers*each {
		t.Fatalf("%d entries, want %d (lost updates)", len(es), len(stores)*workers*each)
	}
	seen := map[string]bool{}
	for _, e := range es {
		if seen[e.ID] {
			t.Fatalf("duplicate id %s", e.ID)
		}
		seen[e.ID] = true
	}
}

// Fix round 1: another dot's unapproved proposal must not rewrite an approved
// convention (plan decision 15, spec 8.2); it may only bump uses.
func TestProposalNeverRewritesApprovedConvention(t *testing.T) {
	s := testStore(t)
	approved := "Services reach the database layer only through repository classes in src/data, never directly from handlers."
	e := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindConvention, Text: approved, Source: SourceModel, Status: StatusProposed})
	if _, err := s.Update(pid, e.Ref(), func(x *Entry) error { x.Status = StatusActive; return nil }); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "frontend", Kind: KindConvention, Source: SourceModel, Status: StatusProposed, Paths: []string{"web/**"},
		Text: "Services reach the storage layer only through repository classes in src/data, never directly from handlers."})
	got, err := s.Get(pid, e.Ref())
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != approved || got.Status != StatusActive || got.Dot != "security" || got.Uses != 1 || len(got.Paths) != 0 {
		t.Fatalf("approved entry changed: %+v", got)
	}
	if n := len(list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusActive, StatusProposed}})); n != 1 {
		t.Fatalf("%d project entries", n)
	}
}

func TestForgetScrubFailureKeepsEntryAndRetrySucceeds(t *testing.T) {
	s := testStore(t)
	e := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindDismissed, Text: "CSRF tokens on /admin are intentionally omitted.", Source: SourceOutcome, Evidence: Evidence{Card: "7"}})
	ledger := filepath.Join(s.Dir, pid, "ledger.jsonl")
	if err := os.Mkdir(ledger, 0o700); err != nil { // unreadable as a ledger
		t.Fatal(err)
	}
	if err := s.Forget(pid, e.Ref(), true); err == nil {
		t.Fatal("want a ledger error")
	}
	if _, err := s.Get(pid, e.Ref()); err != nil {
		t.Fatalf("entry must survive a failed scrub: %v", err)
	}
	os.Remove(ledger)
	dir := filepath.Join(s.Dir, pid)
	if err := s.withLock(dir, func() error {
		return s.appendLedgerLocked(pid, Outcome{Dot: "security", Card: "7", Kind: OutcomeDismissed, Body: "CSRF tokens on /admin are intentionally omitted."})
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(pid, e.Ref(), true); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if led, _ := s.Ledger(pid); len(led) != 0 {
		t.Fatalf("ledger %+v", led)
	}
	if _, err := s.Get(pid, e.Ref()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("entry still there: %v", err)
	}
}

func TestSeeProjectSetsAsideCorruptMetaAndWrapsVersion(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.Dir, pid)
	os.MkdirAll(dir, 0o700)
	meta := filepath.Join(dir, "meta.json")
	os.WriteFile(meta, []byte("{oops"), 0o600)
	if err := s.SeeProject(Project{ID: pid, Name: "herdr-dots", Root: "/r"}); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(meta + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt meta not set aside: %v", matches)
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "{oops" {
		t.Fatal("set-aside file lost the original bytes")
	}
	if ps, err := s.Projects(); err != nil || len(ps) != 1 || ps[0].Name != "herdr-dots" {
		t.Fatalf("%+v %v", ps, err)
	}
	future := []byte(`{"version":2,"id":"` + pid + `"}`)
	os.WriteFile(meta, future, 0o600)
	err := s.SeeProject(Project{ID: pid, Name: "x"})
	if !errors.Is(err, ErrVersion) || !strings.Contains(err.Error(), "meta.json") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(meta); !bytes.Equal(b, future) {
		t.Fatal("newer meta overwritten")
	}
}

func TestProjectsSurfacesLockAndCorruptMetaErrors(t *testing.T) {
	s := testStore(t)
	if err := s.SeeProject(Project{ID: pid, Name: "herdr-dots", Root: "/r"}); err != nil {
		t.Fatal(err)
	}
	s.Limits.LockTimeout = 100 * time.Millisecond
	held, err := lockfile.TryLock(filepath.Join(s.Dir, pid, "memory.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Projects(); !errors.Is(err, ErrLocked) {
		t.Fatalf("Projects: %v", err)
	}
	if _, ok, err := s.FindProject(pid); ok || !errors.Is(err, ErrLocked) {
		t.Fatalf("FindProject: %v %v", ok, err)
	}
	held.Release()
	os.WriteFile(filepath.Join(s.Dir, pid, "meta.json"), []byte("{oops"), 0o600)
	ps, err := s.Projects()
	if !errors.Is(err, ErrCorrupt) || len(ps) != 1 || ps[0].ID != pid {
		t.Fatalf("%+v %v", ps, err)
	}
}

// Fix round 2: one project's damaged meta.json must not block lookups of
// another, nor of the damaged project itself (so it can still be purged).
func TestFindProjectIgnoresOtherProjectsCorruptMeta(t *testing.T) {
	s := testStore(t)
	a, b := pid, "p-00000000000000ff"
	root := t.TempDir()
	for _, pr := range []Project{{ID: a, Name: "a", Root: realPath(root)}, {ID: b, Name: "b", Root: "/b"}} {
		if err := s.SeeProject(pr); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(s.Dir, b, "meta.json"), []byte("{oops"), 0o600)
	if m, ok, err := s.FindProject(a); !ok || err != nil || m.Name != "a" {
		t.Fatalf("by id: %+v %v %v", m, ok, err)
	}
	if m, ok, err := s.FindProject(root); !ok || err != nil || m.ID != a {
		t.Fatalf("by path: %+v %v %v", m, ok, err)
	}
	if m, ok, err := s.FindProject(b); !ok || err != nil || m.ID != b {
		t.Fatalf("damaged project by id: %+v %v %v", m, ok, err)
	}
	if _, ok, err := s.FindProject(t.TempDir()); ok || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("no match should explain the damage: %v %v", ok, err)
	}
}
