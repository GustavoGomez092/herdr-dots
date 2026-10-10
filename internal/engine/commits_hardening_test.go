package engine

import (
	"context"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
)

// R4: one dot attached to two panes in the same repository reviews a new
// commit once, not once per pane (spec §6.3: each on_commit dot reviews
// every new HEAD once).
func TestOneDotOnTwoPanesReviewsACommitOnce(t *testing.T) {
	r, repo := commitRig(t, commitDot("Tests"))
	const other = "w1:p2"
	r.srv.SetPane(herdr.PaneInfo{PaneID: other, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: repo,
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s2"}})
	if err := r.e.Attach(context.Background(), other, []string{"tests"}); err != nil {
		t.Fatal(err)
	}
	if !r.srv.WaitSubscribed(other, time.Second) {
		t.Fatal("engine did not subscribe to the second pane")
	}
	r.checkHeads(t) // the second pane records the current HEAD
	if n := len(commitRuns(r)); n != 0 {
		t.Fatalf("%d runs before any new commit", n)
	}
	r.be.set(proResult("warn", "CheckCSRF has no test."))
	proCommit(t, repo, "auth.go", "package auth\n\nfunc CheckCSRF() bool { return true }\n", "add CSRF check")
	r.checkHeads(t)
	eventually(t, "a commit review", func() bool { return len(commitRuns(r)) >= 1 })
	eventually(t, "the reviews to end", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.bgQueued) == 0
	})
	r.checkHeads(t)
	if n := len(commitRuns(r)); n != 1 {
		t.Fatalf("one dot on two panes reviewed the commit %d times", n)
	}
}
