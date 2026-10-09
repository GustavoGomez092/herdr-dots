package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
)

// Final review item 4: a chair on a kind with no default model and no
// chair_model fails clearly, naming [council] chair_model, and is never
// called (no charge).
func TestChairWithoutModelFailsWithoutACall(t *testing.T) {
	for _, kind := range []string{dots.KindOpenAIAPI, dots.KindOpenRouter, dots.KindOllama} {
		t.Run(kind, func(t *testing.T) {
			r, cb := councilRig(t, func(c *config.Config) { c.Council.ChairBackend = kind },
				member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
			seat(t, r, "security", "frontend")
			cb.on("Security", says("warn", "JWT in localStorage."))
			cb.on("Frontend", says("warn", "No loading state."))
			convene(t, r, "")
			c := councilCard(t, r)
			councilDone(t, r)
			if n := len(cb.callsFor(council.ChairName)); n != 0 {
				t.Fatalf("the chair was called %d times with no model", n)
			}
			if !strings.Contains(c.Note, "merged without chair") || !strings.Contains(c.Note, "[council] chair_model") ||
				strings.Contains(c.Note, "[backend] model") {
				t.Fatalf("note %q", c.Note)
			}
			if got := r.usage.Calls(pane, "council"); got != 0 {
				t.Fatalf("a chair that never ran was charged %d calls", got)
			}
		})
	}
}

// With chair_model set, the same kinds run the chair.
func TestChairModelIsUsedForKindsWithoutADefault(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.Council.ChairBackend, c.Council.ChairModel = dots.KindOllama, "qwen3:30b" },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("warn", "a"))
	cb.on("Frontend", says("warn", "b"))
	cb.on(council.ChairName, says("warn", goodDigest))
	convene(t, r, "")
	councilCard(t, r)
	councilDone(t, r)
	if ch := cb.callsFor(council.ChairName); len(ch) != 1 || ch[0].Model != "qwen3:30b" {
		t.Fatalf("chair requests %+v", ch)
	}
}

// The preflight shows the chair is unavailable and doesn't count its call.
func TestPreflightExcludesAnUnavailableChair(t *testing.T) {
	r, _ := councilRig(t, func(c *config.Config) { c.Council.ChairBackend = dots.KindOpenAIAPI },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	pf, err := r.e.CouncilPreflight(pane, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pf.Chair || pf.ChairUnavailable == "" || !strings.Contains(pf.ChairUnavailable, "chair_model") || pf.EstimatedCalls != 2 {
		t.Fatalf("preflight %+v", pf)
	}
}

// Loading a config whose chair needs a model logs once, not on every reload.
func TestChairModelMissingIsLoggedOnce(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	r := newRigWith(t, func(d *Deps) {
		d.Config.Council.ChairBackend = dots.KindOpenRouter
		d.Backend = func(string) (backend.Backend, error) { return &fakeBackend{}, nil }
		d.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	})
	r.e.noteChairConfig(r.e.config())
	r.e.noteChairConfig(r.e.config())
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, l := range logs {
		if strings.Contains(l, "chair_model") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want one chair_model log line, got %d: %v", n, logs)
	}
}

func TestChairSpec(t *testing.T) {
	cfg := config.Default()
	if k, m, err := chairSpec(cfg); k != "claude-cli" || m != "sonnet" || err != nil {
		t.Fatalf("default: %s %s %v", k, m, err)
	}
	cfg.Council.ChairBackend = dots.KindAnthropicAPI
	if _, m, err := chairSpec(cfg); m != dots.DefaultAnthropicModel || err != nil {
		t.Fatalf("anthropic: %s %v", m, err)
	}
	cfg.Council.ChairBackend = dots.KindOpenAIAPI
	if _, _, err := chairSpec(cfg); err == nil || !strings.Contains(err.Error(), "[council] chair_model") {
		t.Fatalf("openai without chair_model: %v", err)
	}
	cfg.DefaultBackend, cfg.DefaultModel, cfg.Council.ChairBackend = dots.KindOpenAIAPI, "gpt-6-sol", ""
	if k, m, err := chairSpec(cfg); k != dots.KindOpenAIAPI || m != "gpt-6-sol" || err != nil {
		t.Fatalf("default backend's model: %s %s %v", k, m, err)
	}
}
