package worktree

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// G12: CaptureMax keeps at most maxBytes+1 bytes of the patch text, so a
// huge draft is never buffered whole; Validate rejects the cut patch as too
// large, never as anything it could parse.
func TestCaptureMaxNeverKeepsMoreThanTheCap(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	s := snapshotT(t, g, repo)
	co := checkoutT(t, g, s)
	writeFile(t, filepath.Join(co.Dir, "big.txt"), strings.Repeat("a line of text\n", 20000)) // about 300 KB
	const max = 4096
	p, err := CaptureMax(context.Background(), g, co, s, max)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Diff) > max+1 || !p.Truncated {
		t.Fatalf("kept %d bytes (cap %d), truncated %v", len(p.Diff), max, p.Truncated)
	}
	err = Validate(context.Background(), g, co, p, Caps{Deny: noDeny, MaxBytes: max})
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "more than 4096 bytes") {
		t.Fatalf("Validate of a cut patch: %v, want ErrTooLarge (more than 4096 bytes)", err)
	}

	// Under the cap, the patch is whole and passes.
	small, err := CaptureMax(context.Background(), g, co, s, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if small.Truncated || small.Bytes != len(small.Diff) || small.Bytes < 300000 {
		t.Fatalf("an uncut patch: %d bytes, truncated %v", small.Bytes, small.Truncated)
	}
	if err := Validate(context.Background(), g, co, small, Caps{Deny: noDeny, MaxBytes: 1 << 20}); err != nil {
		t.Fatal(err)
	}
}
