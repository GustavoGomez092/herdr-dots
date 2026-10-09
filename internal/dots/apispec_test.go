package dots

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func apiDot(kind, model string) Dot {
	d := New("API")
	d.Purpose = "p"
	d.Backend = BackendSpec{Kind: kind, Model: model, Args: []string{}}
	return d
}

func TestAPIKindsValidate(t *testing.T) {
	ok := []Dot{
		apiDot(KindAnthropicAPI, ""), // default model
		apiDot(KindOpenAIAPI, "gpt-6-sol"),
		apiDot(KindOpenRouter, "anthropic/claude-opus-5-5"),
		apiDot(KindOllama, "qwen3-coder:30b"),
	}
	ok[0].Backend.Effort, ok[0].Backend.Fallbacks, ok[0].Backend.MaxTokens = "low", "off", 8000
	ok[1].Backend.Effort = "xhigh"
	ok[2].Backend.BaseURL = "https://openrouter.ai/api/v1"
	ok[3].Backend.BaseURL, ok[3].Backend.ContextWindow = "http://127.0.0.1:11434", 32768
	ok[3].Budget.MaxUSDPerSession = 0.5
	for _, d := range ok {
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.Backend.Kind, err)
		}
	}
}

func TestAPIFieldsRejectedPerKind(t *testing.T) {
	cases := []struct {
		mut  func(*Dot)
		want string
	}{
		{func(d *Dot) { d.Backend.Kind, d.Backend.Model, d.Backend.Effort = KindOllama, "m", "low" }, "effort applies"},
		{func(d *Dot) { d.Backend.Effort = "turbo" }, "effort \"turbo\""},
		{func(d *Dot) { d.Backend.BaseURL = "http://x" }, "base_url applies"},
		{func(d *Dot) { d.Backend.Kind, d.Backend.Model = KindOpenAIAPI, "" }, "model is required"},
		{func(d *Dot) { d.Backend.Kind, d.Backend.Model, d.Backend.BaseURL = KindOllama, "m", "file:///etc" }, "http(s) URL"},
		{func(d *Dot) {
			d.Backend.Kind, d.Backend.Model, d.Backend.BaseURL = KindOpenRouter, "m", "https://u:p@h"
		}, "credentials"},
		{func(d *Dot) { d.Backend.Kind, d.Backend.Model, d.Backend.ContextWindow = KindOllama, "m", 1000 }, "context_window must be"},
		{func(d *Dot) { d.Backend.ContextWindow = 4096 }, "context_window applies"},
		{func(d *Dot) { d.Backend.Kind, d.Backend.Model, d.Backend.Fallbacks = KindOpenAIAPI, "m", "off" }, "fallbacks applies"},
		{func(d *Dot) { d.Backend.Fallbacks = "maybe" }, "fallbacks \"maybe\""},
		{func(d *Dot) { d.Backend.Kind = "claude-cli"; d.Backend.MaxTokens = 100 }, "max_tokens applies"},
		{func(d *Dot) { d.Backend.MaxTokens = 100000 }, "max_tokens must be"},
		{func(d *Dot) { d.Backend.Args = []string{"--verbose"} }, "CLI backends only"},
		{func(d *Dot) { d.Budget.MaxUSDPerSession = -1 }, "max_usd_per_session"},
		{func(d *Dot) { d.Budget.MaxUSDPerSession = math.NaN() }, "max_usd_per_session"},
	}
	for _, c := range cases {
		d := apiDot(KindAnthropicAPI, "")
		c.mut(&d)
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("want %q, got %v", c.want, err)
		}
	}
}

func TestDefaultModelFor(t *testing.T) {
	if DefaultModelFor("claude-cli") != "sonnet" || DefaultModelFor(KindAnthropicAPI) != "claude-opus-5-5" || DefaultModelFor(KindOpenAIAPI) != "" {
		t.Fatal("DefaultModelFor")
	}
}

func TestAPIDotRoundTripsAndV1FilesStayClean(t *testing.T) {
	dir := t.TempDir()
	d := apiDot(KindOllama, "qwen3-coder:30b")
	d.Backend.ContextWindow = 32768
	d.Budget.MaxUSDPerSession = 0.25
	if err := Save(dir, d, true); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "api")
	if err != nil || got.Backend.ContextWindow != 32768 || got.Budget.MaxUSDPerSession != 0.25 {
		t.Fatalf("%+v %v", got.Backend, err)
	}
	v1 := New("Old")
	v1.Purpose = "p"
	if err := Save(dir, v1, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "old.toml"))
	for _, k := range []string{"effort", "max_tokens", "base_url", "context_window", "fallbacks", "max_usd_per_session"} {
		if strings.Contains(string(raw), k) {
			t.Errorf("a CLI dot's file must not gain %q:\n%s", k, raw)
		}
	}
}
