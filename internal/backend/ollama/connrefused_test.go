package ollama

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// A stopped Ollama must get the "isn't running" hint on every OS; Windows
// reports WSAECONNREFUSED, which syscall.ECONNREFUSED doesn't match.
func TestMapErrNamesAStoppedServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, derr := net.DialTimeout("tcp", addr, 2*time.Second)
	if derr == nil {
		t.Skip("something is listening on the closed port")
	}
	got := mapErr(fmt.Errorf("post /api/chat: %w", derr), "http://"+addr, "llama3.2")
	if got == nil || !strings.Contains(got.Error(), "isn't running") {
		t.Fatalf("mapErr = %v", got)
	}
}
