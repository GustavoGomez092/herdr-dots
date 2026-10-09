package engine

import (
	"context"
	"testing"
)

// The library carries the purpose of the trusted copy that runs, so the
// profile shows what runs, not the file as it is now; an untrusted file runs
// nothing and carries none.
func TestLibraryCarriesTheTrustedCopysPurpose(t *testing.T) {
	r := newProjRig(t)
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.libDot(helperSlug); d.Purpose != "" {
		t.Fatalf("an untrusted dot carries a purpose: %q", d.Purpose)
	}
	r.trustAs(t)
	if d, _ := r.libDot(helperSlug); !d.Trusted || d.Purpose != "Be helpful." {
		t.Fatalf("trusted entry %+v", d)
	}
}
