package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// V6 (Task 1): the 0.7 suppression threshold must rarely fire on genuinely
// different advice. testdata/similar_pairs.tsv is hand-labeled:
// label<TAB>new verdict message<TAB>remembered entry text.
func TestSuppressionThresholdOnLabeledPairs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "similar_pairs.tsv"))
	if err != nil {
		t.Fatalf("Task 1 must create testdata/similar_pairs.tsv: %v", err)
	}
	var same, diff, hits, falsePos int
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if i == 0 {
			continue // header
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("line %d: want 3 tab-separated columns, got %d", i+1, len(cols))
		}
		score := SuppressScore(cols[1], cols[2])
		switch cols[0] {
		case "same":
			same++
			if score >= SuppressThreshold {
				hits++
			}
		case "different":
			diff++
			if score >= SuppressThreshold {
				falsePos++
				t.Logf("false positive %.2f: %q vs %q", score, cols[1], cols[2])
			}
		default:
			t.Fatalf("line %d: label %q", i+1, cols[0])
		}
	}
	if same+diff < 30 || same == 0 || diff == 0 {
		t.Fatalf("need at least 30 labeled pairs of both kinds, have %d same / %d different", same, diff)
	}
	t.Logf("recall %d/%d, false positives %d/%d", hits, same, falsePos, diff)
	if float64(falsePos) > 0.10*float64(diff) {
		t.Fatalf("false-positive rate %d/%d is over 10%%: revisit SuppressThreshold with the user", falsePos, diff)
	}
}

// V3 (Task 1): recorded real replies parse, and their ops apply cleanly.
func TestRecordedRepliesParseAndApply(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "replies", "*.txt"))
	if len(files) == 0 {
		t.Fatal("Task 1 must record replies in testdata/replies/")
	}
	s := testStore(t)
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		var ops []backend.MemoryOp
		var err error
		if strings.HasPrefix(filepath.Base(f), "pass-") {
			ops, err = backend.ParseMemoryOps(string(raw))
		} else {
			_, ops, err = backend.ParseReply(string(raw))
		}
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		res, err := s.Apply(pid, "security", ops, Evidence{Turn: 1})
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		t.Logf("%s: %d ops, applied %d, rejected %v", filepath.Base(f), len(ops), res.Applied(), res.Rejected)
	}
}
