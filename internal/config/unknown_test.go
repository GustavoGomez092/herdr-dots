package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// L4/L20 (post-rc hardening): keys config.toml sets that no setting reads
// (a typo) and [providers.x] tables for providers that don't exist are
// recorded by name, so the caller can warn once; the rest of the file still
// applies.
func TestLoadRecordsUnknownKeysAndProviders(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	body := strings.Join([]string{
		"max_parallel_runs = 5",
		"inject_enabeld = false",
		"[council]",
		"roundz = 2",
		"[providers.anthropic]",
		"base_url = \"https://example.test\"",
		"api_keyenv = \"X\"",
		"[providers.antropic]",
		"api_key_env = \"MY_KEY\"",
	}, "\n")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"council.roundz", "inject_enabeld", "providers.anthropic.api_keyenv", "providers.antropic"}
	if !reflect.DeepEqual(c.UnknownKeys, want) {
		t.Fatalf("unknown keys = %q, want %q", c.UnknownKeys, want)
	}
	if c.MaxParallelRuns != 5 || !c.InjectEnabled {
		t.Fatalf("the known keys still apply: %+v", c)
	}
}

func TestLoadWithOnlyKnownKeysRecordsNone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	body := "max_parallel_runs = 5\n[mcp]\ntools = []\n[providers.ollama]\nbase_url = \"http://localhost:11434\"\n[providers.openai.extra_headers]\nX-Org = \"o\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(p); err != nil || c.UnknownKeys != nil {
		t.Fatalf("unknown keys = %q, err %v", c.UnknownKeys, err)
	}
}
