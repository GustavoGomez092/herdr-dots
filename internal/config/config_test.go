package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || !reflect.DeepEqual(c, Default()) {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestLoadPartialFileMergesOverDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = 5\ninject_enabled = false\n"), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxParallelRuns != 5 || c.InjectEnabled || c.DefaultBackend != "claude-cli" || c.SessionBudgetCalls != 120 {
		t.Fatalf("merged = %+v", c)
	}
}

func TestLoadClampsNonsense(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = 0\nrun_timeout_seconds = 1\nturn_debounce_ms = -5\nsession_budget_calls = 0\n"), 0o600)
	c, _ := Load(p)
	if c.MaxParallelRuns != 1 || c.RunTimeoutSeconds != 10 || c.TurnDebounceMs != 0 || c.SessionBudgetCalls != 1 {
		t.Fatalf("clamped = %+v", c)
	}
}

// The inject kill switch fails closed: a malformed file disables inject.
func TestLoadBadTOMLReturnsDefaultsWithInjectOffAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("inject_enabled = false\nmax_parallel_runs = = 3"), 0o600)
	c, err := Load(p)
	want := Default()
	want.InjectEnabled, want.MemoryEnabled = false, false
	want.ProactiveEnabled = false
	if err == nil || !reflect.DeepEqual(c, want) {
		t.Fatalf("want defaults with inject and memory off + error, got %+v %v", c, err)
	}
}

func TestDurations(t *testing.T) {
	c := Default()
	if c.RunTimeout() != 120*time.Second || c.Debounce() != 1500*time.Millisecond {
		t.Fatal("durations")
	}
}

func TestCouncilDefaultsAndClamps(t *testing.T) {
	if c := Default().Council; c != (CouncilConfig{Rounds: 1, MaxMembers: 8, Merge: MergeChair}) {
		t.Fatalf("defaults %+v", c)
	}
	if (CouncilConfig{}).Normalized() != DefaultCouncil() {
		t.Fatal("an empty [council] table means the defaults")
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[council]\nchair_backend = \"codex-cli\"\nchair_model = \"gpt-x\"\nrounds = 2\nmerge = \"plain\"\n"), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := (CouncilConfig{ChairBackend: "codex-cli", ChairModel: "gpt-x", Rounds: 2, MaxMembers: 8, Merge: MergePlain}); c.Council != want {
		t.Fatalf("council %+v, want %+v", c.Council, want)
	}
	os.WriteFile(p, []byte("[council]\nrounds = 7\nmax_members = 1\nmerge = \"vote\"\n"), 0o600)
	c, _ = Load(p)
	if want := (CouncilConfig{Rounds: 2, MaxMembers: 2, Merge: MergeChair}); c.Council != want {
		t.Fatalf("clamped %+v, want %+v", c.Council, want)
	}
	os.WriteFile(p, []byte("[council]\nmax_members = 99\nrounds = -1\n"), 0o600)
	c, _ = Load(p)
	if c.Council.MaxMembers != 16 || c.Council.Rounds != 1 {
		t.Fatalf("clamped %+v", c.Council)
	}
}

func TestPhase4SettingsLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	body := `session_budget_usd = 2.5
tool_deny_globs = ["**/fixtures/private/**", "[bad"]
[providers.anthropic]
api_key_env = "WORK_ANTHROPIC"
[providers.ollama]
base_url = "http://10.0.0.5:11434"
[providers.openrouter.extra_headers]
"X-Title" = "Herdr Dots"
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionBudgetUSD != 2.5 || c.Provider("anthropic").APIKeyEnv != "WORK_ANTHROPIC" ||
		c.Provider("ollama").BaseURL != "http://10.0.0.5:11434" || c.Provider("openrouter").ExtraHeaders["X-Title"] != "Herdr Dots" {
		t.Fatalf("config %+v", c)
	}
	if len(c.ToolDenyGlobs) != 1 || c.ToolDenyGlobs[0] != "**/fixtures/private/**" {
		t.Fatalf("invalid globs are dropped: %v", c.ToolDenyGlobs)
	}
	if c.Provider("openai").BaseURL != "" {
		t.Fatal("absent provider is the zero value")
	}
}

func TestNegativeDollarBudgetIsOff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(p, []byte("session_budget_usd = -3\n"), 0o600)
	if c, _ := Load(p); c.SessionBudgetUSD != 0 {
		t.Fatalf("got %v", c.SessionBudgetUSD)
	}
	if Default().SessionBudgetUSD != 0 {
		t.Fatal("dollar budgets are off by default")
	}
}

func loadBody(t *testing.T, body string) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestAPIKeyEnvMustBeAVariableName(t *testing.T) {
	c, err := loadBody(t, "[providers.anthropic]\napi_key_env = \"sk-ant-abc123\"\n[providers.openai]\napi_key_env = \"WORK_OPENAI_1\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Provider("anthropic").APIKeyEnv; got != "" {
		t.Fatalf("literal key not cleared: %q", got)
	}
	if got := c.Provider("openai").APIKeyEnv; got != "WORK_OPENAI_1" {
		t.Fatalf("valid name lost: %q", got)
	}
}

func TestProviderBaseURLIsValidated(t *testing.T) {
	for _, bad := range []string{"ftp://h", "http://user:pw@h", "http://:80", "not a url", "/rel"} {
		c, err := loadBody(t, "[providers.ollama]\nbase_url = \""+bad+"\"\n")
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Provider("ollama").BaseURL; got != "" {
			t.Errorf("%q kept as %q", bad, got)
		}
	}
	c, _ := loadBody(t, "[providers.ollama]\nbase_url = \"https://example.com:8443\"\n")
	if c.Provider("ollama").BaseURL != "https://example.com:8443" {
		t.Fatalf("valid url dropped: %q", c.Provider("ollama").BaseURL)
	}
}

func TestSecretHeadersAreDropped(t *testing.T) {
	c, err := loadBody(t, "[providers.openrouter.extra_headers]\n\"Authorization\" = \"Bearer x\"\n\"X-API-Key\" = \"k\"\n\"api-key\" = \"k\"\n\"Proxy-Authorization\" = \"p\"\n\"X-Title\" = \"Herdr Dots\"\n")
	if err != nil {
		t.Fatal(err)
	}
	h := c.Provider("openrouter").ExtraHeaders
	if len(h) != 1 || h["X-Title"] != "Herdr Dots" {
		t.Fatalf("headers %v", h)
	}
}

func TestNonFiniteBudgetIsOff(t *testing.T) {
	for _, v := range []string{"nan", "inf", "-inf"} {
		c, _ := loadBody(t, "session_budget_usd = "+v+"\n")
		if c.SessionBudgetUSD != 0 {
			t.Errorf("%s -> %v", v, c.SessionBudgetUSD)
		}
	}
}

func TestMalformedConfigLeavesPhase4Defaults(t *testing.T) {
	c, err := loadBody(t, "session_budget_usd = 5\n[providers.x\n")
	if err == nil || c.SessionBudgetUSD != 0 || c.Providers != nil || c.ToolDenyGlobs != nil {
		t.Fatalf("got %+v %v", c, err)
	}
}
