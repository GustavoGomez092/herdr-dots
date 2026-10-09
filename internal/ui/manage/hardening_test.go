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

func newProvModel(store *fakeKeys, test func(context.Context, string) error) tea.Model {
	m := tea.Model(New("", Deps{Caller: &fakeCaller{}, Keys: store, TestKey: test}))
	return press(m, "K")
}

func TestEditorErrorsAreRedacted(t *testing.T) {
	const leaked = "sk-ant-abcdef123456789"
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(),
		CheckDot: func(context.Context, dots.Dot) (string, error) { return "", errors.New("401 for " + leaked) },
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
	if v := m.View(); strings.Contains(v, leaked) || !strings.Contains(v, "401") {
		t.Fatalf("save error:\n%s", v)
	}
	m, _ = m.Update(modelsMsg{kind: dots.KindAnthropicAPI, err: errors.New("bad key " + leaked)})
	if v := m.View(); strings.Contains(v, leaked) || !strings.Contains(v, "bad key") {
		t.Fatalf("models error:\n%s", v)
	}
}

func TestSetErrorEchoingTheTypedKeyIsRedacted(t *testing.T) {
	const typed = "abc123secretvalue"
	store := &fakeKeys{keys: map[string]string{}, setErr: func(k string) error { return errors.New("rejected " + k) }}
	m := newProvModel(store, nil)
	m = press(m, "s")
	m = typeText(m, typed)
	m = press(m, "enter")
	if v := m.View(); strings.Contains(v, typed) || !strings.Contains(v, "rejected") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestTestResultClearedOnRemoveAndStore(t *testing.T) {
	store := &fakeKeys{keys: map[string]string{"anthropic": "k-old-1111"}}
	m := newProvModel(store, func(context.Context, string) error { return nil })
	m = press(m, "t")
	if !strings.Contains(m.View(), "✓ verified") {
		t.Fatalf("setup:\n%s", m.View())
	}
	m, _ = m.Update(key("x"))
	if got := m.(Model).prov.tested["anthropic"]; got != "" {
		t.Fatalf("remove must clear the test result, got %q", got)
	}
	m.(Model).prov.tested["anthropic"] = "✓ verified 00:00"
	m = press(m, "s")
	m = typeText(m, "k-new-2222")
	m, _ = m.Update(key("enter"))
	if got := m.(Model).prov.tested["anthropic"]; got != "" {
		t.Fatalf("storing a key must clear the old result, got %q", got)
	}
}

func TestListErrorKeepsRowsAndCursor(t *testing.T) {
	store := &fakeKeys{keys: map[string]string{}}
	m := newProvModel(store, nil)
	m = press(m, "j")
	store.listErr = errors.New("disk exploded")
	m, _ = m.Update(m.(Model).loadProviders()())
	v := m.View()
	if !strings.Contains(v, "anthropic") || !strings.Contains(v, "openai") || !strings.Contains(v, "disk exploded") {
		t.Fatalf("rows kept and error shown:\n%s", v)
	}
	if got := m.(Model).prov.rows[m.(Model).prov.cursor].Provider; got != "openai" {
		t.Fatalf("cursor moved to %s", got)
	}
}

func TestEnteringKeyIsPinnedToTheProviderWhereSWasPressed(t *testing.T) {
	store := &fakeKeys{keys: map[string]string{}}
	m := newProvModel(store, nil)
	m = press(m, "s")
	m = typeText(m, "pinned-key-0001")
	m.(Model).prov.cursor = 1 // would be openai if the cursor decided
	m = press(m, "enter")
	if store.keys["anthropic"] != "pinned-key-0001" || len(store.keys) != 1 {
		t.Fatalf("stored %v", store.keys)
	}
}

func TestReloadKeepsCursorOnTheSameProvider(t *testing.T) {
	m := newProvModel(&fakeKeys{keys: map[string]string{}}, nil)
	m = press(m, "j") // openai
	rows := []credentials.Status{{Provider: "openrouter"}, {Provider: "openai"}, {Provider: "anthropic"}}
	m, _ = m.Update(providersMsg{rows: rows})
	if got := m.(Model).prov.rows[m.(Model).prov.cursor].Provider; got != "openai" {
		t.Fatalf("cursor followed the index, now on %s", got)
	}
}

func TestFailedRemoveKeepsRows(t *testing.T) {
	store := &fakeKeys{keys: map[string]string{"anthropic": "k-1234"}, rmErr: errors.New("read-only file")}
	m := newProvModel(store, nil)
	m = press(m, "x")
	v := m.View()
	if !strings.Contains(v, "anthropic") || !strings.Contains(v, "openrouter") || !strings.Contains(v, "read-only file") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestEscClearsTheKeyBuffer(t *testing.T) {
	m := newProvModel(&fakeKeys{keys: map[string]string{}}, nil)
	m = press(m, "s")
	m = typeText(m, "half-typed-secret")
	m = press(m, "esc")
	if got := m.(Model).prov.input.Value(); got != "" {
		t.Fatalf("buffer kept %q", got)
	}
	if !strings.Contains(m.View(), "PROVIDERS") {
		t.Fatal("esc while entering stays on the providers screen")
	}
}

func TestPastedKeyIsTrimmed(t *testing.T) {
	store := &fakeKeys{keys: map[string]string{}}
	m := newProvModel(store, nil)
	m = press(m, "s")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  " + secret + "\n"), Paste: true})
	m = press(m, "enter")
	if store.keys["anthropic"] != secret {
		t.Fatalf("stored %q", store.keys["anthropic"])
	}
}

func TestStaleModelsErrorAndRepeatedCtrlSpace(t *testing.T) {
	d := Deps{Caller: &fakeCaller{}, Models: func(context.Context, string, string) ([]string, error) {
		return []string{"m1"}, nil
	}, NewDot: func() dots.Dot {
		d := dots.New("")
		d.Backend = dots.BackendSpec{Kind: dots.KindAnthropicAPI, Args: []string{}}
		return d
	}}
	m := tea.Model(New("", d))
	m = press(m, "n")
	focusOn(m.(Model).form, "Model")
	var c1, c2, c3 tea.Cmd
	m, c1 = m.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	m, c2 = m.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	if c1 == nil || c2 != nil {
		t.Fatalf("a press during a fetch must be ignored: %v %v", c1 != nil, c2 != nil)
	}
	m, _ = m.Update(modelsMsg{kind: dots.KindOllama, err: errors.New("ollama down")})
	if strings.Contains(m.View(), "ollama down") {
		t.Fatal("an error for a kind the user left must not show")
	}
	m, _ = m.Update(c1())
	m, c3 = m.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	if c3 != nil {
		t.Fatal("after the fetch lands, ctrl+space cycles instead of refetching")
	}
}

func TestSwitchingBetweenURLKindsResetsBaseURL(t *testing.T) {
	d := dots.New("x")
	d.Backend = dots.BackendSpec{Kind: dots.KindOllama, Model: "q", Args: []string{}, BaseURL: "http://gpu:11434"}
	f := NewForm(d, false)
	cycleKindTo(t, f, dots.KindOpenRouter)
	if got := f.val(iBaseURL); got != "" {
		t.Fatalf("base_url carried from ollama to openrouter: %q", got)
	}
	f.fields[iBaseURL].input.SetValue("https://proxy.example")
	cycleKindTo(t, f, dots.KindAnthropicAPI)
	cycleKindTo(t, f, dots.KindOllama)
	if got := f.val(iBaseURL); got != "" {
		t.Fatalf("hidden base_url survived a round trip: %q", got)
	}
}
