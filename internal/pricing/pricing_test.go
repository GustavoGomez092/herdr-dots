package pricing

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestEmbeddedTableParsesWithDate(t *testing.T) {
	d := Default()
	if d.AsOf != "2026-10-08" {
		t.Fatalf("as_of = %q", d.AsOf)
	}
	p, ok := d.Lookup("anthropic", "claude-opus-5-5")
	if !ok || p.Input != 4 || p.Output != 20 || p.CacheRead != 0.20 || p.CacheWrite != 5 {
		t.Fatalf("opus 5.5 = %+v %v", p, ok)
	}
	if len(d.Prices["openai"]) == 0 {
		t.Fatal("openai entries missing")
	}
}

func TestCostArithmeticWithCache(t *testing.T) {
	// 10k uncached in, 2k out, 50k cache read, 4k cache write on Opus 5.5:
	// 10000*4 + 2000*20 + 50000*0.2 + 4000*5 = 40000+40000+10000+20000 = 110000 / 1e6
	usd, ok := Default().Cost("anthropic", "claude-opus-5-5", Usage{In: 10000, Out: 2000, CacheRead: 50000, CacheWrite: 4000})
	if !ok || !near(usd, 0.11) {
		t.Fatalf("cost = %v %v, want 0.11", usd, ok)
	}
}

func TestMissingCachePricesFallBackToInput(t *testing.T) {
	usd, ok := Default().Cost("anthropic", "claude-opus-5", Usage{CacheRead: 1_000_000})
	if !ok || !near(usd, 5) {
		t.Fatalf("cost = %v, want the input price", usd)
	}
}

func TestUnknownModelIsNotKnown(t *testing.T) {
	usd, ok := Default().Cost("openai", "gpt-unlisted", Usage{In: 1000})
	if ok || usd != 0 {
		t.Fatalf("got %v %v", usd, ok)
	}
	if _, ok := Default().Cost("ollama", "qwen3", Usage{In: 1}); ok {
		t.Fatal("ollama has no table entries")
	}
}

func TestLoadMergesOverride(t *testing.T) {
	p := filepath.Join(t.TempDir(), "prices.toml")
	if err := os.WriteFile(p, []byte("as_of = \"2026-11-01\"\n[openai.\"gpt-7\"]\ninput = 1.5\noutput = 6\n[anthropic.\"claude-opus-5-5\"]\ninput = 3\noutput = 15\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tb, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if tb.AsOf != "2026-11-01" {
		t.Fatalf("as_of = %q", tb.AsOf)
	}
	if x, ok := tb.Lookup("openai", "gpt-7"); !ok || x.Input != 1.5 {
		t.Fatalf("new model: %+v", x)
	}
	if x, _ := tb.Lookup("anthropic", "claude-opus-5-5"); x.Input != 3 || x.CacheRead != 0 {
		t.Fatalf("override replaces the whole entry: %+v", x)
	}
	if _, ok := tb.Lookup("anthropic", "claude-haiku-4-5"); !ok {
		t.Fatal("untouched entries stay")
	}
}

func TestLoadMissingAndMalformed(t *testing.T) {
	if tb, err := Load(filepath.Join(t.TempDir(), "none.toml")); err != nil || tb.AsOf != "2026-10-08" {
		t.Fatalf("missing: %v", err)
	}
	p := filepath.Join(t.TempDir(), "bad.toml")
	_ = os.WriteFile(p, []byte("[openai.\"x\"]\ninput = -1\n"), 0o600)
	tb, err := Load(p)
	if err == nil {
		t.Fatal("negative price must be an error")
	}
	if _, ok := tb.Lookup("anthropic", "claude-opus-5-5"); !ok {
		t.Fatal("a bad override keeps the embedded table")
	}
}

func loadSrc(t *testing.T, src string) (Table, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prices.toml")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestEmptyOverrideEntryIsRejected(t *testing.T) {
	tb, err := loadSrc(t, "[openai.\"gpt-7\"]\n")
	if err == nil || !strings.Contains(err.Error(), "gpt-7") {
		t.Fatalf("want an error naming gpt-7, got %v", err)
	}
	if _, ok := tb.Lookup("openai", "gpt-7"); ok {
		t.Fatal("an empty entry must not become a known $0 price")
	}
	if _, ok := tb.Lookup("anthropic", "claude-opus-5-5"); !ok {
		t.Fatal("embedded table must survive")
	}
}

func TestTypoedKeyIsRejected(t *testing.T) {
	_, err := loadSrc(t, "[openai.\"gpt-7\"]\ninptu = 2\noutput = 6\n")
	if err == nil || !strings.Contains(err.Error(), "gpt-7") || !strings.Contains(err.Error(), "inptu") {
		t.Fatalf("want an error naming gpt-7 and inptu, got %v", err)
	}
}

func TestValidOverrideStillLoads(t *testing.T) {
	tb, err := loadSrc(t, "[openai.\"gpt-7\"]\ninput = 2\noutput = 6\ncache_read = 0.2\n")
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := tb.Lookup("openai", "gpt-7"); !ok || p.Input != 2 || p.CacheRead != 0.2 {
		t.Fatalf("%+v %v", p, ok)
	}
}

func TestNegativeUsageNeverMakesCostNegative(t *testing.T) {
	usd, ok := Default().Cost("anthropic", "claude-opus-5-5", Usage{In: -1000, Out: -5, CacheRead: -1, CacheWrite: -1})
	if !ok || usd != 0 {
		t.Fatalf("cost = %v %v, want 0", usd, ok)
	}
}

func TestEstimate(t *testing.T) {
	usd, ok := Default().Estimate("anthropic", "claude-haiku-4-5", 3)
	// (1200 + 4500) * 1 + 300 * 5 = 5700 + 1500 = 7200 / 1e6
	if !ok || !near(usd, 0.0072) {
		t.Fatalf("estimate = %v", usd)
	}
}
