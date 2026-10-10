//go:build !windows

package memui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"herdr-dots/internal/memory"
)

// Fix round 1: I on a FIFO is refused without opening it (opening a FIFO
// with no writer blocks forever).
func TestImportRefusesAFIFOWithoutHanging(t *testing.T) {
	st, cwd := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits()), t.TempDir()
	fifo := filepath.Join(cwd, "pipe.toml")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	m := press(openAt(st, herdrDots, "", cwd), "I", "ctrl+u", fifo)
	m, cmd := m.Update(key("enter"))
	done := make(chan Model, 1)
	go func() { done <- run(m, cmd).(Model) }()
	select {
	case got := <-done:
		if got.mode == modeImportPreview || !strings.Contains(got.status, "not a regular file") {
			t.Fatalf("status %q", got.status)
		}
	case <-time.After(3 * time.Second):
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close() // let the blocked reader go
		}
		t.Fatal("I hangs on a FIFO")
	}
}
