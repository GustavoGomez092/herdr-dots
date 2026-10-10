package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L4/L20 (post-rc hardening): the engine's config loader warns once per set
// of unknown config.toml keys, naming each key, never their values.
func TestConfigLoaderWarnsOnceNamingUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var logs []string
	load := configLoaderLogged(path, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	write("inject_enabeld = \"sk-proj-INFILE0123456789\"\n[providers.antropic]\napi_key_env = \"K\"\n")
	load()
	load()
	if len(logs) != 1 || !strings.Contains(logs[0], `"inject_enabeld"`) || !strings.Contains(logs[0], `"providers.antropic"`) || strings.Contains(logs[0], "INFILE") {
		t.Fatalf("want one line naming both keys, got %q", logs)
	}
	write("max_parallel_runs = 2\n")
	load()
	write("inject_enabeld = false\n")
	load()
	if len(logs) != 2 || !strings.Contains(logs[1], `"inject_enabeld"`) {
		t.Fatalf("a fixed then re-broken file warns again: %q", logs)
	}
}
