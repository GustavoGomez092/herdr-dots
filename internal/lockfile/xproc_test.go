package lockfile

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLockHolderChild takes the lock, says so, and holds it until stdin
// closes. It is the other process for the test below.
func TestLockHolderChild(t *testing.T) {
	p := os.Getenv("LOCKFILE_CHILD_PATH")
	if p == "" {
		t.Skip("helper process for TestLockIsExclusiveAcrossProcesses")
	}
	l, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("LOCKED\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	l.Release()
}

func TestLockIsExclusiveAcrossProcesses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.lock")
	child := exec.Command(os.Args[0], "-test.run=^TestLockHolderChild$", "-test.count=1", "-test.v")
	child.Env = append(os.Environ(), "LOCKFILE_CHILD_PATH="+p)
	stdin, _ := child.StdinPipe()
	stdout, _ := child.StdoutPipe()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	locked := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "LOCKED") {
				locked <- true
			}
		}
		close(locked)
	}()
	select {
	case ok := <-locked:
		if !ok {
			t.Fatal("child never took the lock")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child never took the lock")
	}
	if !Held(p) {
		t.Fatal("Held must see the other process's lock")
	}
	if _, err := TryLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("TryLock while the child holds it: %v", err)
	}
	stdin.Close()
	if err := child.Wait(); err != nil {
		t.Fatalf("child: %v", err)
	}
	l, err := Acquire(p, 2*time.Second)
	if err != nil {
		t.Fatalf("lock not free after the child exited: %v", err)
	}
	l.Release()
}
