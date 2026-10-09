package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// TestRecordedChairReplies replays the real chair replies the Task 1 spike
// recorded (V1). verified-*.txt must parse as a verdict and pass ParseDigest;
// unverified-*.txt parse as a verdict but fail ParseDigest (the card then says
// "unverified digest"); malformed-*.txt fail backend.ParseVerdict (the engine
// then uses the plain merge).
func TestRecordedChairReplies(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "chair", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 10 {
		t.Fatalf("the Task 1 spike records at least 10 chair replies; found %d", len(files))
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(f)
		v, perr := backend.ParseVerdict(string(raw))
		switch {
		case strings.HasPrefix(base, "malformed-"):
			if perr == nil {
				t.Errorf("%s parses as a verdict; rename it", base)
			}
		case strings.HasPrefix(base, "verified-"), strings.HasPrefix(base, "unverified-"):
			if perr != nil {
				t.Errorf("%s: %v", base, perr)
				continue
			}
			if _, ok := ParseDigest(v.Message); ok != strings.HasPrefix(base, "verified-") {
				t.Errorf("%s: ParseDigest verified=%v\n%s", base, ok, v.Message)
			}
		default:
			t.Errorf("%s: name must start with verified-, unverified- or malformed-", base)
		}
	}
}
