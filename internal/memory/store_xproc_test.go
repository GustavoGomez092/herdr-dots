package memory

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Two engines on the same repo are two processes: the flock must serialize
// them with zero lost updates. The test binary re-runs itself as the writers.
func TestCrossProcessWritersLoseNoUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	const procs, each = 2, 100
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*bytes.Buffer, procs)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestXProcWriterChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), "MEMORY_XPROC_DIR="+dir, "MEMORY_XPROC_ID="+strconv.Itoa(i), "MEMORY_XPROC_N="+strconv.Itoa(each))
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
	es, err := Open(dir, DefaultLimits()).List(pid, Filter{Scope: ScopeDot, Dot: "security"})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != procs*each {
		t.Fatalf("%d entries, want %d (lost updates across processes)", len(es), procs*each)
	}
}

// TestXProcWriterChild is the writer process for the test above.
func TestXProcWriterChild(t *testing.T) {
	dir := os.Getenv("MEMORY_XPROC_DIR")
	if dir == "" {
		t.Skip("helper process for TestCrossProcessWritersLoseNoUpdates")
	}
	id := os.Getenv("MEMORY_XPROC_ID")
	n, _ := strconv.Atoi(os.Getenv("MEMORY_XPROC_N"))
	lim := DefaultLimits()
	lim.MaxDot, lim.LockTimeout = 100000, 60*time.Second
	s := Open(dir, lim)
	for i := 0; i < n; i++ {
		text := fmt.Sprintf("proc%s item%d unique%s%04d", id, i, id, i)
		if _, err := s.Add(pid, dotNote("security", text)); err != nil {
			t.Fatal(err)
		}
	}
}
