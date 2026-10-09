package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProactiveDefaults(t *testing.T) {
	c := Default()
	if !c.ProactiveEnabled || c.DailyBudgetUSD != 1.00 || c.CommitPollSeconds != 15 {
		t.Fatalf("%+v", c)
	}
}

func TestProactiveSettingsOverrideAndClamp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("proactive_enabled = false\ndaily_budget_usd = 2.5\ncommit_poll_seconds = 30\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.ProactiveEnabled || c.DailyBudgetUSD != 2.5 || c.CommitPollSeconds != 30 {
		t.Fatalf("%+v %v", c, err)
	}
	for _, body := range []string{"daily_budget_usd = 0\n", "daily_budget_usd = -3\n", "daily_budget_usd = nan\n", "daily_budget_usd = inf\n"} {
		os.WriteFile(p, []byte(body), 0o600)
		if c, err := Load(p); err != nil || c.DailyBudgetUSD != DefaultDailyBudgetUSD {
			t.Fatalf("%q: budget %v %v, want the default (never unlimited)", body, c.DailyBudgetUSD, err)
		}
	}
	os.WriteFile(p, []byte("commit_poll_seconds = 1\n"), 0o600)
	if c, _ := Load(p); c.CommitPollSeconds != 5 {
		t.Fatalf("low poll clamps to 5: %d", c.CommitPollSeconds)
	}
	os.WriteFile(p, []byte("commit_poll_seconds = 99999\n"), 0o600)
	if c, _ := Load(p); c.CommitPollSeconds != 600 {
		t.Fatalf("high poll clamps to 600: %d", c.CommitPollSeconds)
	}
}

// proactive_enabled is a safety switch: a malformed or unreadable file turns
// background work off, like inject and memory.
func TestProactiveKillSwitchFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("proactive_enabled = true\nmax_parallel_runs = = 3"), 0o600)
	if c, err := Load(p); err == nil || c.ProactiveEnabled {
		t.Fatalf("malformed: %+v %v", c, err)
	}
	if c, err := Load(t.TempDir()); err == nil || c.ProactiveEnabled {
		t.Fatalf("unreadable: %+v %v", c, err)
	}
}
