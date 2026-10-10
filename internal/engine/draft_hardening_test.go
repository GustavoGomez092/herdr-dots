package engine

import (
	"os"
	"testing"
)

// D3: the draft sweep removes a patch file with no record (a crash between
// PutPatch's two writes).
func TestTheDraftSweepRemovesAPatchFileWithoutARecord(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	if err := os.MkdirAll(r.patches.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := r.patches.PatchPath("0123456789abcdef")
	if err := os.WriteFile(orphan, []byte("diff --git a/x b/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.maintainDrafts()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphan patch file stayed: %v", err)
	}
}
