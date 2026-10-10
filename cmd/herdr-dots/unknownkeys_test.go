package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// L4/L20 (post-rc hardening): the backends' config loader keeps the unknown
// keys on the config it returns but logs nothing about them: the engine logs
// and toasts them once per set (Engine.noteUnknownConfig), and the daemon
// log shares one logf, so a line here would double it.
func TestConfigLoaderLeavesUnknownKeysToTheEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("inject_enabeld = \"sk-proj-INFILE0123456789\"\n[providers.antropic]\napi_key_env = \"K\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs []string
	load := configLoaderLogged(path, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	c := load()
	if want := []string{"inject_enabeld", "providers.antropic"}; !reflect.DeepEqual(c.UnknownKeys, want) {
		t.Fatalf("unknown keys %q, want %q", c.UnknownKeys, want)
	}
	if len(logs) != 0 {
		t.Fatalf("the loader logged %q; unknown keys are the engine's to log", logs)
	}
}
