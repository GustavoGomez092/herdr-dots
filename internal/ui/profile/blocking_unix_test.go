//go:build !windows

package profile

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/history"
	"herdr-dots/internal/view"
)

// The dot's history file is a FIFO nobody writes to: any read of it blocks
// forever. New, Update and View must return without touching it; only the
// Init commands may.
func TestViewAndUpdateNeverTouchABlockingHistoryFile(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "security.jsonl"), 0o600); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	hs := history.Open(dir)
	done := make(chan string, 1)
	go func() {
		m := New(Deps{History: hs, Now: func() time.Time { return pNow }, Dot: view.LibraryDot{Slug: "security", Name: "Security"}})
		_ = m.Init() // builds the commands without running them
		var v string
		for _, msg := range []tea.Msg{tea.WindowSizeMsg{Width: 80, Height: 24}, pkey("tab"), pkey("j"), pkey("enter"), pkey("esc"), pkey("x")} {
			m, _ = m.Update(msg)
			v = m.View()
		}
		done <- v
	}()
	select {
	case v := <-done:
		if !strings.Contains(v, "loading") {
			t.Fatalf("expected the loading state:\n%s", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("View or Update touched the history file")
	}
}
