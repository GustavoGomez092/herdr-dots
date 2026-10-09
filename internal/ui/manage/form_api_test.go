package manage

import (
	"strings"
	"testing"

	"herdr-dots/internal/dots"
)

func labels(f *Form) string {
	var out []string
	for i, fl := range f.fields {
		if f.visible(i) {
			out = append(out, fl.label)
		}
	}
	return strings.Join(out, "|")
}

func cycleKindTo(t *testing.T, f *Form, kind string) {
	t.Helper()
	focusOn(f, "Backend")
	for i := 0; i < len(dots.BackendKinds) && f.Kind() != kind; i++ {
		f.Update(k("right"))
	}
	if f.Kind() != kind {
		t.Fatalf("could not select %s", kind)
	}
}

func TestKindCycleShowsAndHidesFields(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	if l := labels(f); strings.Contains(l, "Effort") || strings.Contains(l, "Base URL") || !strings.Contains(l, "Budget ($/session") {
		t.Fatalf("claude-cli fields: %s", l)
	}
	cycleKindTo(t, f, dots.KindAnthropicAPI)
	if l := labels(f); !strings.Contains(l, "Effort") || !strings.Contains(l, "Refusal fallbacks") || strings.Contains(l, "Base URL") || strings.Contains(l, "Context window") {
		t.Fatalf("anthropic-api fields: %s", l)
	}
	cycleKindTo(t, f, dots.KindOllama)
	if l := labels(f); !strings.Contains(l, "Base URL") || !strings.Contains(l, "Context window") || strings.Contains(l, "Effort") {
		t.Fatalf("ollama fields: %s", l)
	}
	if v := f.View(80); strings.Contains(v, "Effort") || !strings.Contains(v, "Context window") {
		t.Fatalf("view shows hidden fields:\n%s", v)
	}
}

func TestTabSkipsHiddenFields(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	for i := 0; i < 3*len(f.fields); i++ {
		f.Update(k("tab"))
		if !f.visible(f.focus) {
			t.Fatalf("focus landed on hidden field %q", f.fields[f.focus].label)
		}
	}
}

func TestKindChangeSwapsOnlyADefaultModel(t *testing.T) {
	f := NewForm(dots.New("x"), true) // claude-cli / sonnet
	cycleKindTo(t, f, dots.KindAnthropicAPI)
	if got := f.val(iModel); got != "claude-opus-5-5" {
		t.Fatalf("model = %q, want the anthropic-api default", got)
	}
	cycleKindTo(t, f, dots.KindOpenAIAPI)
	if got := f.val(iModel); got != "" {
		t.Fatalf("openai has no default model, got %q", got)
	}
	g := NewForm(dots.New("y"), true)
	focusOn(g, "Model")
	g.fields[iModel].input.SetValue("opus")
	cycleKindTo(t, g, dots.KindAnthropicAPI)
	if got := g.val(iModel); got != "opus" {
		t.Fatalf("a model the user typed is kept, got %q", got)
	}
}

func TestAPIFieldsRoundTripAndHiddenOnesAreCleared(t *testing.T) {
	d := dots.New("Local")
	d.Purpose = "p"
	d.Backend = dots.BackendSpec{Kind: dots.KindOllama, Model: "qwen3", Args: []string{}, BaseURL: "http://gpu:11434", ContextWindow: 32768}
	d.Budget.MaxUSDPerSession = 0.25
	got, err := NewForm(d, false).Dot()
	if err != nil || got.Backend.BaseURL != "http://gpu:11434" || got.Backend.ContextWindow != 32768 || got.Budget.MaxUSDPerSession != 0.25 {
		t.Fatalf("%+v %v", got, err)
	}
	f := NewForm(d, false)
	cycleKindTo(t, f, dots.KindAnthropicAPI)
	focusOn(f, "Effort")
	f.Update(k("right")) // default → low
	got, err = f.Dot()
	if err != nil || got.Backend.BaseURL != "" || got.Backend.ContextWindow != 0 || got.Backend.Effort != "low" {
		t.Fatalf("after switching kind: %+v %v", got.Backend, err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("switched dot must validate: %v", err)
	}
}

func TestBadDollarBudgetIsAFormError(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	focusOn(f, "Budget ($/session, 0 = off)")
	f.fields[iBudgetUSD].input.SetValue("lots")
	if _, err := f.Dot(); err == nil || !strings.Contains(err.Error(), "number like 0.50") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateLineAndModelPicker(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	cycleKindTo(t, f, dots.KindOpenAIAPI)
	f.SetEstimator(func(d dots.Dot) string {
		if d.Backend.Model == "gpt-unpriced" {
			return "price unknown: dollar budgets won't count this dot"
		}
		return "est. $0.004/run at 3 turns"
	})
	focusOn(f, "Model")
	if f.NextModel() {
		t.Fatal("no choices loaded yet")
	}
	f.SetModels(dots.KindOpenAIAPI, []string{"gpt-6-sol", "gpt-unpriced"})
	if f.val(iModel) != "gpt-6-sol" || !strings.Contains(f.View(80), "est. $0.004/run") {
		t.Fatalf("first model + estimate:\n%s", f.View(80))
	}
	if !f.NextModel() || f.val(iModel) != "gpt-unpriced" || !strings.Contains(f.View(80), "price unknown") {
		t.Fatalf("cycle + unknown price:\n%s", f.View(80))
	}
	f.SetModels(dots.KindOllama, []string{"llama3"}) // stale kind: ignored
	if f.val(iModel) != "gpt-unpriced" {
		t.Fatal("choices for another kind are ignored")
	}
}
