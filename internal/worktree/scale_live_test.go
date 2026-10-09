//go:build live

package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// V4: snapshot time and private-index size on a large repository. Run with
// go test -tags live -run TestSnapshotScale -v ./internal/worktree/
// (HERDR_DOTS_SCALE_FILES sets the tracked file count, default 100000).
func TestSnapshotScale(t *testing.T) {
	n := 100000
	if v, err := strconv.Atoi(os.Getenv("HERDR_DOTS_SCALE_FILES")); err == nil && v > 0 {
		n = v
	}
	g := isoGit(t)
	repo := t.TempDir()
	sh(t, repo, "init", "-q")
	writeFile(t, filepath.Join(repo, ".gitignore"), "node_modules/\n")
	for i := range n {
		writeFile(t, filepath.Join(repo, "src", strconv.Itoa(i%100), fmt.Sprintf("f%d.txt", i)), "line "+strconv.Itoa(i)+"\n")
	}
	for i := range n / 2 {
		writeFile(t, filepath.Join(repo, "node_modules", strconv.Itoa(i%100), fmt.Sprintf("m%d.js", i)), "ignored\n")
	}
	sh(t, repo, "add", ".")
	sh(t, repo, "commit", "-qm", "big")
	writeFile(t, filepath.Join(repo, "src", "0", "f0.txt"), "changed\n")
	for _, run := range []string{"first", "second"} {
		start := time.Now()
		s, err := TakeSnapshot(context.Background(), g, repo, NewID(), 0)
		if err != nil {
			t.Fatal(err)
		}
		took := time.Since(start)
		idx, _ := os.Stat(filepath.Join(repo, ".git", "index"))
		start = time.Now()
		co, err := Add(context.Background(), g, s, filepath.Join(t.TempDir(), s.ID))
		if err != nil {
			t.Fatal(err)
		}
		checkout := time.Since(start)
		_ = Remove(context.Background(), g, repo, co.Dir)
		t.Logf("V4 %s run: %d tracked files (+%d ignored): snapshot %s, checkout %s, index %d KB", run, n, n/2, took.Round(time.Millisecond), checkout.Round(time.Millisecond), idx.Size()>>10)
	}
}
