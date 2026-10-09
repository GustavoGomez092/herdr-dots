package queue

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform/platformtest"
)

func inboxRig(t *testing.T) (*Inbox, *Store, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := Open(filepath.Join(dir, "session", "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "inbox.json")
	return OpenInbox(p), q, p
}

func TestInboxPutListAndClaim(t *testing.T) {
	in, q, p := inboxRig(t)
	if cs, err := in.List("g-0123456789abcdef"); err != nil || len(cs) != 0 {
		t.Fatalf("a missing inbox is empty: %v %v", cs, err)
	}
	for _, c := range []Card{
		{Dot: "security", DotName: "Security", Severity: "warn", Body: "old commit has no test", Mode: "inject", Project: "g-0123456789abcdef", Origin: "scheduled", Label: "scheduled Mon 09:00", Status: StatusAuto, Approved: true, PaneID: "w9:p9"},
		{Dot: "security", Body: "other project", Mode: "queue", Project: "g-fedcba9876543210", Origin: "scheduled"},
	} {
		if err := in.Put(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := in.Put(Card{Body: "no project"}); err == nil {
		t.Fatal("a card without a project can't wait in the inbox")
	}
	if err := in.Put(Card{Kind: KindPatch, PatchID: "0123456789abcdef", Body: "x", Project: "g-0123456789abcdef"}); err == nil {
		t.Fatal("a Phase 7 patch card never leaves its session")
	}
	platformtest.AssertPrivateFile(t, p)
	cs, _ := in.List("g-0123456789abcdef")
	if len(cs) != 1 || cs[0].PaneID != "" || cs[0].Status != StatusPending || cs[0].Approved || !strings.HasPrefix(cs[0].ID, "inbox-") {
		t.Fatalf("inbox card %+v", cs)
	}
	n, err := q.Claim(in, "g-0123456789abcdef", "w1:p1")
	if err != nil || n != 1 {
		t.Fatalf("claim %d %v", n, err)
	}
	got := q.Pending("w1:p1")
	if len(got) != 1 || got[0].Status != StatusPending || got[0].Approved || got[0].Label != "scheduled Mon 09:00" || got[0].Origin != "scheduled" || got[0].Project != "g-0123456789abcdef" {
		t.Fatalf("claimed card %+v", got)
	}
	if left, _ := in.List("g-0123456789abcdef"); len(left) != 0 {
		t.Fatalf("claimed cards leave the inbox: %+v", left)
	}
	if left, _ := in.List("g-fedcba9876543210"); len(left) != 1 {
		t.Fatalf("other projects keep theirs: %+v", left)
	}
	if n, err := q.Claim(in, "g-0123456789abcdef", "w1:p1"); n != 0 || err != nil {
		t.Fatalf("second claim %d %v", n, err)
	}
	if n, err := q.Claim(nil, "g-0123456789abcdef", "w1:p1"); n != 0 || err != nil {
		t.Fatal("a nil inbox claims nothing")
	}
}

func TestInboxKeepsFiftyPerProjectAndDropsOldCards(t *testing.T) {
	in, _, p := inboxRig(t)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	in.Now = func() time.Time { return now }
	for i := 0; i < 55; i++ {
		_ = in.Put(Card{Body: fmt.Sprintf("card %d", i), Project: "g-0123456789abcdef"})
	}
	cs, _ := in.List("g-0123456789abcdef")
	if len(cs) != 50 || cs[0].Body != "card 5" || cs[49].Body != "card 54" {
		t.Fatalf("%d cards, first %q", len(cs), cs[0].Body)
	}
	now = now.Add(31 * 24 * time.Hour)
	_ = in.Put(Card{Body: "fresh", Project: "g-fedcba9876543210"})
	if cs, _ := in.List("g-0123456789abcdef"); len(cs) != 0 {
		t.Fatalf("cards older than 30 days are dropped: %d", len(cs))
	}
	var f inboxFile
	raw, _ := os.ReadFile(p)
	if json.Unmarshal(raw, &f) != nil || len(f.Cards) != 1 || f.Next != 56 {
		t.Fatalf("file %s", raw)
	}
}

func TestDigestAndOriginFieldsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "queue.json")
	s, _ := Open(p)
	c, err := s.Add(Card{Kind: KindDigest, Dot: "away", DotName: "Away", Glyph: "☾", PaneID: "w1:p1", Severity: "warn",
		Body: "## Should fix\n- x (Security)", Mode: "queue", Origin: "research", Label: "2 dots · 42 min",
		Parts: []CardPart{{Dot: "security", DotName: "Security", Glyph: "◆", Severity: "warn", Body: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := Open(p)
	got, ok := s2.Get(c.ID)
	if !ok || got.Kind != KindDigest || got.Origin != "research" || got.Label != "2 dots · 42 min" || len(got.Parts) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestInboxNeverHoldsPatchCards(t *testing.T) {
	in, q, p := inboxRig(t)
	if err := in.Put(Card{Kind: KindPatch, Project: "g-0123456789abcdef", Body: "x"}); err == nil {
		t.Fatal("Put must refuse a patch card")
	}
	// A hand-edited inbox file with a patch card must not leak into a queue.
	raw := `{"next":1,"cards":[{"id":"inbox-1","kind":"patch","patch_id":"0123456789abcdef","project":"g-0123456789abcdef","body":"x","status":"pending","created_at":"2099-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := q.Claim(in, "g-0123456789abcdef", "w1:p1")
	if err != nil || n != 0 || len(q.All()) != 0 {
		t.Fatalf("a patch card must never be claimed: %d %v %+v", n, err, q.All())
	}
}

func TestInboxAndAddSanitizeModelText(t *testing.T) {
	in, q, _ := inboxRig(t)
	err := in.Put(Card{Kind: KindDigest, Project: "g-0123456789abcdef", Label: "scheduled\x1b]52;c;AAAA\x07 Mon", Body: "ok\x1b[2J",
		Parts: []CardPart{{Dot: "a", DotName: "A\x1b[31m", Body: "part\x07 body"}}})
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := in.List("g-0123456789abcdef")
	if len(cs) != 1 || strings.ContainsAny(cs[0].Label+cs[0].Body+cs[0].Parts[0].Body+cs[0].Parts[0].DotName, "\x1b\x07") {
		t.Fatalf("control characters stored: %+v", cs)
	}
	c, _ := q.Add(Card{Kind: KindDigest, PaneID: "w1:p1", Label: "a\x1bb", Parts: []CardPart{{Body: "c\x07d"}}})
	if c.Label != "ab" || c.Parts[0].Body != "cd" {
		t.Fatalf("Add must sanitize too: %+v", c)
	}
}

func TestOldQueueFileWithoutNewFieldsLoads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "queue.json")
	old := `{"next":1,"cards":[{"id":"1","dot":"security","pane_id":"w1:p1","body":"b","mode":"queue","status":"pending","created_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := s.Get("1")
	if !ok || c.Origin != "" || c.Label != "" || c.Kind != "" || c.Body != "b" {
		t.Fatalf("%+v", c)
	}
}

// TestInboxClaimChild claims in a separate process and prints the count.
func TestInboxClaimChild(t *testing.T) {
	dir := os.Getenv("INBOX_CHILD_DIR")
	if dir == "" {
		t.Skip("helper process for TestInboxClaimHasOneWinnerAcrossProcesses")
	}
	q, err := Open(filepath.Join(dir, os.Getenv("INBOX_CHILD_NAME"), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	in := OpenInbox(filepath.Join(dir, "inbox.json"))
	in.Timeout = 20 * time.Second
	// Start barrier: wait for the go-file so every child claims at once.
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, "go")); err == nil {
			break
		}
	}
	n, err := q.Claim(in, "g-0123456789abcdef", "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CLAIMED %d\n", n)
}

func TestInboxClaimHasOneWinnerAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	in := OpenInbox(filepath.Join(dir, "inbox.json"))
	for i := 0; i < 5; i++ {
		if err := in.Put(Card{Body: fmt.Sprintf("c%d", i), Project: "g-0123456789abcdef"}); err != nil {
			t.Fatal(err)
		}
	}
	const procs = 4
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*strings.Builder, procs)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=^TestInboxClaimChild$")
		cmds[i].Env = append(os.Environ(), "INBOX_CHILD_DIR="+dir, fmt.Sprintf("INBOX_CHILD_NAME=s%d", i))
		outs[i] = &strings.Builder{}
		cmds[i].Stdout = outs[i]
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	winners, total := 0, 0
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("child %d: %v\n%s", i, err, outs[i])
		}
		n := -1
		for _, l := range strings.Split(outs[i].String(), "\n") {
			if _, err := fmt.Sscanf(l, "CLAIMED %d", &n); err == nil {
				break
			}
		}
		if n < 0 {
			t.Fatalf("child %d printed no count: %q", i, outs[i])
		}
		if n > 0 {
			winners++
		}
		total += n
		q, _ := Open(filepath.Join(dir, fmt.Sprintf("s%d", i), "queue.json"))
		if got := len(q.All()); got != n {
			t.Fatalf("child %d claimed %d but holds %d", i, n, got)
		}
	}
	if winners != 1 || total != 5 {
		t.Fatalf("want exactly one winner of all 5 cards, got %d winners, %d cards", winners, total)
	}
	if left, _ := in.List("g-0123456789abcdef"); len(left) != 0 {
		t.Fatalf("inbox not empty: %+v", left)
	}
}

const proj = "g-0123456789abcdef"

func putN(t *testing.T, in *Inbox, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := in.Put(Card{Body: fmt.Sprintf("c%d", i), Project: proj}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClaimCrashAfterMarkKeepsCardsAndRecoversAfterTTL(t *testing.T) {
	in, q, _ := inboxRig(t)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	in.Now = func() time.Time { return now }
	putN(t, in, 3)
	// Claimer A marks, then "crashes": it never adds or deletes.
	if got, err := in.mark(proj, "crashed-session"); err != nil || len(got) != 3 {
		t.Fatalf("mark %d %v", len(got), err)
	}
	if cs, _ := in.List(proj); len(cs) != 3 {
		t.Fatalf("a marked card stays in the inbox: %d", len(cs))
	}
	now = now.Add(4 * time.Minute)
	if n, err := q.Claim(in, proj, "w1:p1"); n != 0 || err != nil {
		t.Fatalf("a live claim is not stolen: %d %v", n, err)
	}
	now = now.Add(2 * time.Minute) // past the 5 minute TTL
	n, err := q.Claim(in, proj, "w1:p1")
	if err != nil || n != 3 {
		t.Fatalf("stale marks are claimable again: %d %v", n, err)
	}
	if n, _ := q.Claim(in, proj, "w1:p1"); n != 0 {
		t.Fatalf("exactly once: second claim got %d", n)
	}
	got := q.Pending("w1:p1")
	if len(got) != 3 || got[0].ClaimedBy != "" || !got[0].ClaimedAt.IsZero() {
		t.Fatalf("claimed cards carry no marks: %+v", got)
	}
	if left, _ := in.List(proj); len(left) != 0 {
		t.Fatalf("inbox not empty: %+v", left)
	}
}

func TestClaimCrashAfterAddBeforeDeleteAddsNoDuplicate(t *testing.T) {
	in, q, _ := inboxRig(t)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	in.Now = func() time.Time { return now }
	putN(t, in, 2)
	cards, err := in.mark(proj, q.token())
	if err != nil || len(cards) != 2 {
		t.Fatal(err, cards)
	}
	for _, c := range cards { // phase (b) done, then the process dies before phase (c)
		c.PaneID, c.SourceID, c.ClaimedBy = "w1:p1", c.ID, ""
		if _, err := q.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(6 * time.Minute)
	n, err := q.Claim(in, proj, "w1:p1")
	if err != nil || n != 0 {
		t.Fatalf("re-claim adds nothing new: %d %v", n, err)
	}
	if got := q.All(); len(got) != 2 {
		t.Fatalf("duplicate cards: %+v", got)
	}
	if left, _ := in.List(proj); len(left) != 0 {
		t.Fatalf("the re-claim still deletes the inbox copies: %+v", left)
	}
}

func TestFailedClaimLeavesInboxIntactAndQueueUnchanged(t *testing.T) {
	dir := t.TempDir()
	qdir := filepath.Join(dir, "session")
	q, err := Open(filepath.Join(qdir, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	in := OpenInbox(filepath.Join(dir, "inbox.json"))
	putN(t, in, 3)
	before, _ := in.List(proj)
	if err := os.WriteFile(qdir, []byte("x"), 0o600); err != nil { // a file where the queue dir should be: save() fails
		t.Fatal(err)
	}
	n, err := q.Claim(in, proj, "w1:p1")
	if err == nil || n != 0 {
		t.Fatalf("claim should fail: %d %v", n, err)
	}
	if len(q.All()) != 0 {
		t.Fatalf("nothing stays in memory un-persisted: %+v", q.All())
	}
	if c, err := q.Add(Card{Body: "z"}); err == nil {
		t.Fatalf("still failing: %+v", c)
	}
	after, _ := in.List(proj)
	if len(after) != 3 {
		t.Fatalf("inbox lost cards: %+v", after)
	}
	for i := range before {
		if after[i].ID != before[i].ID || !after[i].CreatedAt.Equal(before[i].CreatedAt) || after[i].Body != before[i].Body || after[i].ClaimedBy != "" {
			t.Fatalf("card %d changed: %+v vs %+v", i, after[i], before[i])
		}
	}
	if err := os.Remove(qdir); err != nil {
		t.Fatal(err)
	}
	n, err = q.Claim(in, proj, "w1:p1")
	if err != nil || n != 3 {
		t.Fatalf("next claim: %d %v", n, err)
	}
	got := q.Pending("w1:p1")
	for i := range before {
		if got[i].Body != before[i].Body || !got[i].CreatedAt.Equal(before[i].CreatedAt) || got[i].SourceID != before[i].ID {
			t.Fatalf("order/CreatedAt/SourceID not kept: %+v vs %+v", got[i], before[i])
		}
	}
}

func TestInboxRefusesUnknownOrigin(t *testing.T) {
	in, _, _ := inboxRig(t)
	for _, o := range []string{"", "research", "scheduled", "commit"} {
		if err := in.Put(Card{Body: "x", Project: proj, Origin: o}); err != nil {
			t.Fatalf("origin %q: %v", o, err)
		}
	}
	if err := in.Put(Card{Body: "x", Project: proj, Origin: "model-said-so"}); err == nil {
		t.Fatal("an unknown origin is refused")
	}
}

func TestInboxSanitizesGlyphDotAndPartFields(t *testing.T) {
	in, _, _ := inboxRig(t)
	err := in.Put(Card{Project: proj, Dot: "a\x1b[2J", Glyph: "g\x07", Body: "b",
		Parts: []CardPart{{Dot: "d\x1b", Glyph: "p\x07", Stance: "s\x1b[0m", Body: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := in.List(proj)
	c := cs[0]
	all := c.Dot + c.Glyph + c.Parts[0].Dot + c.Parts[0].Glyph + c.Parts[0].Stance
	if strings.ContainsAny(all, "\x1b\x07") {
		t.Fatalf("control characters stored: %+v", c)
	}
}

func TestPutMovesACorruptInboxAsideAndStoresTheResult(t *testing.T) {
	in, _, p := inboxRig(t)
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := in.Put(Card{Body: "paid result", Project: proj}); err != nil {
		t.Fatalf("a corrupt inbox must not drop the new result: %v", err)
	}
	if cs, _ := in.List(proj); len(cs) != 1 || cs[0].Body != "paid result" {
		t.Fatalf("%+v", cs)
	}
	m, _ := filepath.Glob(p + ".corrupt-*")
	if len(m) != 1 {
		t.Fatalf("corrupt file not kept aside: %v", m)
	}
	if b, _ := os.ReadFile(m[0]); string(b) != "{not json" {
		t.Fatalf("moved file content %q", b)
	}
}
