package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// stdoutOf runs fn with os.Stdout captured.
func stdoutOf(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	ferr := fn()
	os.Stdout = old
	w.Close()
	out := <-done
	r.Close()
	return out, ferr
}

// L17 (post-rc hardening fix): `memory --help` (and `memory <sub> -h`)
// prints the usage even where the plugin dirs can't be found, and a bare
// `memory` is an error that shows the usage, not a discovery error.
func TestMemoryHelpWorksWithoutThePluginDirs(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "relative/config") // Discover fails without running herdr
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "relative/state")
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"list", "-h"}, {"add", "--scope", "dot", "--help"}} {
		out, err := stdoutOf(t, func() error { return runMemory(args) })
		if err != nil || !strings.Contains(out, "usage: herdr-dots memory") {
			t.Errorf("memory %q: err %v, stdout %q", args, err, out)
		}
	}
	out, err := stdoutOf(t, func() error { return runMemory(nil) })
	if err == nil || !strings.Contains(err.Error(), "usage: herdr-dots memory") || strings.Contains(err.Error(), "not absolute") || out != "" {
		t.Errorf("bare memory: err %v, stdout %q; want the usage as the error", err, out)
	}
	if _, err := stdoutOf(t, func() error { return runMemory([]string{"list"}) }); err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Errorf("memory list without the dirs: err %v, want the discovery error", err)
	}
}
