package history

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"herdr-dots/internal/state"
)

// Two engines (or an engine and the CLI) append to one dot's history from two
// processes: the lock must keep every line whole and lose none.
func TestCrossProcessAppendsLoseNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	const procs, each = 2, 150
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*bytes.Buffer, procs)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHistoryXProcChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), "HISTORY_XPROC_DIR="+dir, "HISTORY_XPROC_ID="+strconv.Itoa(i), "HISTORY_XPROC_N="+strconv.Itoa(each))
		outs[i] = &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = outs[i], outs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("writer %d: %v\n%s", i, err, outs[i])
		}
	}
	st, err := Open(dir).Stats("security", time.Now().Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalRuns() != procs*each || st.Skipped != 0 {
		t.Fatalf("runs %d skipped %d, want %d and 0", st.TotalRuns(), st.Skipped, procs*each)
	}
}

// TestHistoryXProcChild is the writer process; it does nothing in a normal run.
func TestHistoryXProcChild(t *testing.T) {
	dir := os.Getenv("HISTORY_XPROC_DIR")
	if dir == "" {
		t.Skip("helper process")
	}
	id := os.Getenv("HISTORY_XPROC_ID")
	n, _ := strconv.Atoi(os.Getenv("HISTORY_XPROC_N"))
	s := Open(dir)
	s.LockTimeout = 10 * time.Second
	for i := 0; i < n; i++ {
		r := Record{T: TypeRun, Dot: "security", Kind: KindReview, Outcome: state.OutcomePass, Card: fmt.Sprintf("p%s/%d", id, i)}
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}
