// Package pricing turns token usage into US dollars from an embedded, dated
// price table that CONFIG_DIR/prices.toml can override entry by entry.
package pricing

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"regexp"

	"github.com/BurntSushi/toml"
)

//go:embed prices.toml
var embedded string

// Usage is one run's tokens. In is uncached input only: adapters subtract
// cache reads (OpenAI, OpenRouter report them inside the input count).
type Usage struct {
	In, Out, CacheRead, CacheWrite int
}

// Add returns the sum of two usages.
func (u Usage) Add(v Usage) Usage {
	return Usage{u.In + v.In, u.Out + v.Out, u.CacheRead + v.CacheRead, u.CacheWrite + v.CacheWrite}
}

// Total is every input token plus output tokens.
func (u Usage) Total() int { return u.In + u.Out + u.CacheRead + u.CacheWrite }

// Price is USD per 1M tokens. Zero cache prices fall back to Input.
type Price struct {
	Input      float64 `toml:"input"`
	Output     float64 `toml:"output"`
	CacheRead  float64 `toml:"cache_read"`
	CacheWrite float64 `toml:"cache_write"`
}

// Table maps provider → model → price.
type Table struct {
	AsOf   string
	Prices map[string]map[string]Price
}

func parse(src string) (Table, error) {
	var raw map[string]toml.Primitive
	md, err := toml.Decode(src, &raw)
	if err != nil {
		return Table{}, err
	}
	t := Table{Prices: map[string]map[string]Price{}}
	for k, v := range raw {
		if k == "as_of" {
			if err := md.PrimitiveDecode(v, &t.AsOf); err != nil {
				return Table{}, fmt.Errorf("as_of: %w", err)
			}
			continue
		}
		m := map[string]Price{}
		if err := md.PrimitiveDecode(v, &m); err != nil {
			return Table{}, fmt.Errorf("[%s]: %w", k, err)
		}
		for model, p := range m {
			if !valid(p) {
				return Table{}, fmt.Errorf("[%s.%q]: prices must be finite and >= 0", k, model)
			}
			if p.Input <= 0 && p.Output <= 0 {
				return Table{}, fmt.Errorf("[%s.%q]: needs a positive input or output price (typo in a key?)", k, model)
			}
		}
		t.Prices[k] = m
	}
	if un := md.Undecoded(); len(un) > 0 {
		return Table{}, fmt.Errorf("unknown key %q in the price table (expected input, output, cache_read, cache_write)", un[0].String())
	}
	return t, nil
}

func valid(p Price) bool {
	for _, f := range []float64{p.Input, p.Output, p.CacheRead, p.CacheWrite} {
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return false
		}
	}
	return true
}

// Default is the embedded table.
func Default() Table {
	t, err := parse(embedded)
	if err != nil {
		panic("pricing: embedded prices.toml: " + err.Error())
	}
	return t
}

// Load returns Default() with the entries of the file at path merged over it
// (per model; the file's as_of wins when set). A missing file is not an
// error; a malformed one returns Default() and the error.
func Load(path string) (Table, error) {
	t := Default()
	if path == "" {
		return t, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	o, err := parse(string(data))
	if err != nil {
		return t, fmt.Errorf("prices %s: %w", path, err)
	}
	if o.AsOf != "" {
		t.AsOf = o.AsOf
	}
	for prov, models := range o.Prices {
		if t.Prices[prov] == nil {
			t.Prices[prov] = map[string]Price{}
		}
		for m, p := range models {
			t.Prices[prov][m] = p
		}
	}
	return t, nil
}

// Lookup returns the price for provider/model.
func (t Table) Lookup(provider, model string) (Price, bool) {
	p, ok := t.Prices[provider][model]
	return p, ok
}

// Cost prices u. known is false (and usd 0) when the model has no price.
func (t Table) Cost(provider, model string, u Usage) (usd float64, known bool) {
	p, ok := t.Lookup(provider, model)
	if !ok {
		return 0, false
	}
	u = Usage{max(0, u.In), max(0, u.Out), max(0, u.CacheRead), max(0, u.CacheWrite)}
	cr, cw := p.CacheRead, p.CacheWrite
	if cr == 0 {
		cr = p.Input
	}
	if cw == 0 {
		cw = p.Input
	}
	usd = (float64(u.In)*p.Input + float64(u.Out)*p.Output + float64(u.CacheRead)*cr + float64(u.CacheWrite)*cw) / 1e6
	return usd, true
}

// datedSuffix is a trailing snapshot date: -YYYY-MM-DD (OpenAI) or -YYYYMMDD
// (Anthropic).
var datedSuffix = regexp.MustCompile(`.-(\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])|\d{4}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01]))$`)

// StripDate drops a trailing snapshot date from a model id:
// "gpt-6-sol-2026-08-01" → "gpt-6-sol", "claude-haiku-4-5-20251001" →
// "claude-haiku-4-5". Ids without one come back unchanged.
func StripDate(model string) string {
	loc := datedSuffix.FindStringIndex(model)
	if loc == nil {
		return model
	}
	return model[:loc[0]+1]
}

// SameModel reports whether two ids name the same model, ignoring a dated
// snapshot suffix on either.
func SameModel(a, b string) bool {
	return a == b || StripDate(a) == StripDate(b)
}

// CostServed prices a run by the id the provider served, then that id without
// a dated snapshot suffix, then the id that was requested. Providers answer
// with snapshot ids ("gpt-6-sol-2026-08-01") the price table doesn't list.
func (t Table) CostServed(provider, served, requested string, u Usage) (usd float64, known bool) {
	for _, m := range []string{served, StripDate(served), requested} {
		if m == "" {
			continue
		}
		if usd, ok := t.Cost(provider, m, u); ok {
			return usd, true
		}
	}
	return 0, false
}

// Estimate prices a typical review run for the editor: ~1,200 tokens of
// system prompt and memory, ~1,500 per conversation turn, ~300 out.
func (t Table) Estimate(provider, model string, turns int) (float64, bool) {
	return t.Cost(provider, model, Usage{In: 1200 + 1500*max(1, turns), Out: 300})
}
