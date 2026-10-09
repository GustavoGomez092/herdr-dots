package manage

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
)

type fakeKeys struct {
	keys    map[string]string
	listErr error
	setErr  func(key string) error // may echo the key
	rmErr   error
}

func (f *fakeKeys) List() ([]credentials.Status, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []credentials.Status
	for _, p := range credentials.Providers {
		s := credentials.Status{Provider: p, From: "missing"}
		if k, ok := f.keys[p]; ok {
			s.From, s.Last4 = "file", credentials.Last4(k)
		}
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeKeys) Set(p, k string) error {
	if f.setErr != nil {
		return f.setErr(k)
	}
	f.keys[p] = k
	return nil
}
func (f *fakeKeys) Remove(p string) error {
	if f.rmErr != nil {
		return f.rmErr
	}
	delete(f.keys, p)
	return nil
}

const secret = "sk-ant-api03-SUPERSECRETVALUE9876"

func typeText(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestProvidersScreenStoresKeyMaskedAndTests(t *testing.T) {
	store := &fakeKeys{keys: map[string]string{}}
	var tested []string
	m := tea.Model(New("", Deps{Caller: &fakeCaller{}, Keys: store,
		TestKey: func(_ context.Context, p string) error { tested = append(tested, p); return nil }}))
	m = press(m, "K")
	if v := m.View(); !strings.Contains(v, "PROVIDERS") || !strings.Contains(v, "anthropic") || !strings.Contains(v, "ollama") {
		t.Fatalf("providers view:\n%s", v)
	}
	m = press(m, "s")
	m = typeText(m, secret)
	if v := m.View(); strings.Contains(v, "SUPERSECRET") || strings.Contains(v, secret) {
		t.Fatalf("the key must never be rendered:\n%s", v)
	}
	m = press(m, "enter")
	if store.keys["anthropic"] != secret {
		t.Fatalf("stored %v", store.keys)
	}
	v := m.View()
	if strings.Contains(v, "SUPERSECRET") || !strings.Contains(v, "…9876") || !strings.Contains(v, "✓ verified") {
		t.Fatalf("after store:\n%s", v)
	}
	if len(tested) != 1 || tested[0] != "anthropic" {
		t.Fatalf("tested %v", tested)
	}
	m = press(m, "x")
	if _, ok := store.keys["anthropic"]; ok || !strings.Contains(m.View(), "missing") {
		t.Fatalf("remove: %v\n%s", store.keys, m.View())
	}
	m = press(m, "esc")
	if !strings.Contains(m.View(), "DOTS LIBRARY") {
		t.Fatal("esc returns to the library")
	}
}

func TestProvidersTestErrorIsRedacted(t *testing.T) {
	m := tea.Model(New("", Deps{Caller: &fakeCaller{}, Keys: &fakeKeys{keys: map[string]string{}},
		TestKey: func(context.Context, string) error { return errors.New("401 for key " + secret) }}))
	m = press(m, "K", "t")
	if v := m.View(); strings.Contains(v, "SUPERSECRET") || !strings.Contains(v, "✗") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestSaveUsesCheckDotWhenSet(t *testing.T) {
	var checked dots.Dot
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(),
		Check: func(context.Context, string) error { t.Fatal("Check must not run when CheckDot is set"); return nil },
		CheckDot: func(_ context.Context, d dots.Dot) (string, error) {
			checked = d
			return "", errors.New("model \"x\" not available to this key")
		},
		NewDot: func() dots.Dot {
			d := dots.New("")
			d.Backend = dots.BackendSpec{Kind: dots.KindAnthropicAPI, Model: "x", Args: []string{}}
			return d
		}}
	m := tea.Model(New("", d))
	m = press(m, "n")
	m = typeText(m, "Api")
	mm := m.(Model)
	mm.form.fields[iPurpose].area.SetValue("p")
	m = press(mm, "ctrl+s")
	if checked.Backend.Model != "x" || !strings.Contains(m.View(), "not available") {
		t.Fatalf("checked %+v view:\n%s", checked.Backend, m.View())
	}
}

// Final review item 2: an unreachable provider is a warning; the dot saves.
func TestSaveGoesThroughWithACheckWarning(t *testing.T) {
	dir := t.TempDir()
	d := Deps{Caller: &fakeCaller{}, DotsDir: dir,
		CheckDot: func(context.Context, dots.Dot) (string, error) {
			return "not checked: Ollama isn't running at http://gpu:11434", nil
		},
		NewDot: func() dots.Dot {
			d := dots.New("")
			d.Backend = dots.BackendSpec{Kind: dots.KindOllama, Model: "llama3", Args: []string{}}
			return d
		}}
	m := tea.Model(New("", d))
	m = press(m, "n")
	m = typeText(m, "Local")
	mm := m.(Model)
	mm.form.fields[iPurpose].area.SetValue("p")
	m = press(mm, "ctrl+s")
	if _, err := dots.Load(dir, "local"); err != nil {
		t.Fatalf("the dot must be saved: %v\n%s", err, m.View())
	}
	if v := m.View(); !strings.Contains(v, "saved local") || !strings.Contains(v, "isn't running") {
		t.Fatalf("view:\n%s", v)
	}
}

// Final review item 2: ctrl+space asks the dot's own base_url.
func TestCtrlSpacePassesTheBaseURL(t *testing.T) {
	var gotBase string
	d := Deps{Caller: &fakeCaller{}, Models: func(_ context.Context, kind, base string) ([]string, error) {
		gotBase = base
		return []string{"llama3:latest"}, nil
	}, NewDot: func() dots.Dot {
		d := dots.New("")
		d.Backend = dots.BackendSpec{Kind: dots.KindOllama, BaseURL: "http://gpu:11434", Args: []string{}}
		return d
	}}
	m := tea.Model(New("", d))
	m = press(m, "n")
	mm := m.(Model)
	focusOn(mm.form, "Model")
	_, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	if cmd == nil {
		t.Fatal("ctrl+space must fetch models")
	}
	cmd()
	if gotBase != "http://gpu:11434" {
		t.Fatalf("base %q", gotBase)
	}
}

func TestCtrlSpaceFetchesModels(t *testing.T) {
	var asked []string
	d := Deps{Caller: &fakeCaller{}, Models: func(_ context.Context, kind, _ string) ([]string, error) {
		asked = append(asked, kind)
		return []string{"claude-haiku-4-5", "claude-opus-5-5"}, nil
	}, NewDot: func() dots.Dot {
		d := dots.New("")
		d.Backend = dots.BackendSpec{Kind: dots.KindAnthropicAPI, Model: "", Args: []string{}}
		return d
	}}
	m := tea.Model(New("", d))
	m = press(m, "n")
	mm := m.(Model)
	focusOn(mm.form, "Model")
	var cmd tea.Cmd
	m, cmd = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	if cmd == nil {
		t.Fatal("ctrl+space must fetch models")
	}
	m, _ = m.Update(cmd())
	if got := m.(Model).form.val(iModel); got != "claude-haiku-4-5" || len(asked) != 1 || asked[0] != dots.KindAnthropicAPI {
		t.Fatalf("model %q asked %v", got, asked)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	if got := m.(Model).form.val(iModel); got != "claude-opus-5-5" || len(asked) != 1 {
		t.Fatalf("second ctrl+space cycles without refetching: %q %v", got, asked)
	}
}
