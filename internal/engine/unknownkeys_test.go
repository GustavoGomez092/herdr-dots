package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/dots"
)

// L4 (post-rc hardening fix): keys config.toml sets that no setting reads
// get one log line and one toast at engine start and on each reload that
// changes the set, and nothing while the set stays the same.
func TestUnknownConfigKeysAreLoggedAndToastedOncePerSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var mu sync.Mutex
	var logs []string
	r := newRigWith(t, func(dp *Deps) {
		dp.Config.UnknownKeys = []string{"inject_enabeld"}
		dp.ConfigPath = path
		dp.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	}, dot("Security", dots.ModeQueue))
	toasts := func() []string {
		var out []string
		for _, c := range r.srv.Calls("notification.show") {
			if b := fmt.Sprint(c.Params["body"]); strings.Contains(b, "unknown key") {
				out = append(out, b)
			}
		}
		return out
	}
	lines := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, l := range logs {
			if strings.Contains(l, "unknown key") {
				out = append(out, l)
			}
		}
		return out
	}
	settle := func(want int) {
		t.Helper()
		eventually(t, "the toasts", func() bool { return len(toasts()) >= want })
		time.Sleep(50 * time.Millisecond)
		if got, l := toasts(), lines(); len(got) != want || len(l) != want {
			t.Fatalf("toasts %q, log lines %q, want %d of each", got, l, want)
		}
	}

	settle(1) // at engine start
	if got := toasts()[0]; !strings.Contains(got, `"inject_enabeld"`) || !strings.Contains(got, "a typo?") {
		t.Fatalf("toast %q", got)
	}
	r.e.noteUnknownConfig(r.e.config()) // the same set again
	settle(1)

	if err := os.WriteFile(path, []byte("inject_enabeld = true\nroundz = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.ReloadConfig() // a changed set
	settle(2)
	if got := toasts()[1]; !strings.Contains(got, `"roundz"`) {
		t.Fatalf("second toast %q", got)
	}
	r.e.ReloadConfig() // unchanged
	settle(2)
}
