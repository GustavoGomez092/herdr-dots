package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
)

// H10: a pruned checkout leaves no scan counter behind.
func TestAPrunedCheckoutLeavesNoScanCounter(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.attach(t, helperSlug)
	r.e.dropPane(pane)
	r.e.mu.Lock()
	_, left := r.e.scanSeq[r.root]
	r.e.mu.Unlock()
	if left {
		t.Fatalf("scan counter of a pruned checkout is still kept")
	}
}

// H10: cleaning the counters must not let a scan that started before a
// checkout was pruned install over the scan made after a pane came back.
func TestAScanFromBeforeAPruneNeverOverwritesALaterOne(t *testing.T) {
	root := projectRepo(t, repoHelperTOML)
	ts := &projectdots.TrustStore{Path: root + "-trusted.json"}
	var mu sync.Mutex
	block := false
	entered, release := make(chan struct{}), make(chan struct{})
	r := newRigFull(t, func(d *Deps) { projectDeps(d, ts, root); noPoll(d) }, func(e *Engine) {
		e.testHook = func(point string) {
			mu.Lock()
			b := block && point == "scan-read"
			if b {
				block = false
			}
			mu.Unlock()
			if b {
				close(entered)
				<-release
			}
		}
	})
	_ = r.e.ScanPane(context.Background(), pane)
	pr := memory.Project{ID: repoProject, Name: "app", Root: root}
	mu.Lock()
	block = true
	mu.Unlock()
	done := make(chan struct{})
	go func() { r.e.scanRoot(pr); close(done) }() // reads the old file, then waits
	<-entered
	r.e.dropPane(pane) // the checkout is pruned
	editHelper(t, root, repoHelperTOML, "Be helpful.", "Be newer.")
	_ = r.e.ScanPane(context.Background(), pane) // a pane is back: a fresh scan installs
	close(release)
	<-done
	want := projectdots.Hex(projectdots.Sum([]byte(strings.Replace(repoHelperTOML, "Be helpful.", "Be newer.", 1))))
	found := false
	for _, d := range r.e.Snapshot().Library {
		if d.Slug == helperSlug {
			found = true
			if d.SHA256 != want {
				t.Fatalf("the scan from before the prune replaced the newer one: %s", d.SHA256)
			}
		}
	}
	if !found {
		t.Fatal("helper not in the library")
	}
}
