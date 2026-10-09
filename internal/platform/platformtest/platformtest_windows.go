//go:build windows

package platformtest

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"herdr-dots/internal/platform"
)

const root = `C:\`

var endpointSeq atomic.Int64

func endpoint(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("%sherdr-dots-test-%d-%d", platform.PipePrefix, os.Getpid(), endpointSeq.Add(1))
}

func assertPrivate(t testing.TB, path string, dir bool) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if st.IsDir() != dir {
		t.Fatalf("%s: dir=%v, want %v", path, st.IsDir(), dir)
	}
	if err := platform.CheckPrivate(path); err != nil {
		t.Fatalf("%s is not private: %v", path, err)
	}
}
