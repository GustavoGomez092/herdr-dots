//go:build windows

package ipc

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"herdr-dots/internal/platform"
)

var pipeSeq atomic.Int64

// testEndpoint is a fresh named pipe path, unique within this test run.
func testEndpoint(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%sherdr-dots-test-%d-%d", platform.PipePrefix, os.Getpid(), pipeSeq.Add(1))
}

func TestListenRefusesANameThatIsAlreadyServed(t *testing.T) {
	path := testEndpoint(t)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if second, err := Listen(path); err == nil {
		second.Close()
		t.Fatal("a second engine must not share the pipe")
	}
}
