package manage

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
)

func k(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func focusOn(f *Form, label string) {
	for f.fields[f.focus].label != label {
		f.Update(k("tab"))
	}
}

func TestFormRoundTripsATemplate(t *testing.T) {
	d, _ := dots.FromTemplate("security")
	d.Trigger.FileGlobs = []string{"**/*.ts", "api/**"}
	d.Backend.Args = []string{"--fallback-model", "haiku"}
	got, err := NewForm(d, false).Dot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("round trip changed the dot:\n got %+v\nwant %+v", got, d)
	}
}

func TestFormEditsFields(t *testing.T) {
	f := NewForm(dots.New(""), true)
	for _, r := range "Perf Hawk" {
		f.Update(k(string(r)))
	}
	focusOn(f, "Mode")
	f.Update(k("right")) // queue → inject
	focusOn(f, "Review every turn")
	f.Update(k("space"))
	focusOn(f, "File globs")
	for _, r := range "a.go, b/** ,," {
		f.Update(k(string(r)))
	}
	focusOn(f, "Purpose")
	for _, r := range "Find slow code." {
		f.Update(k(string(r)))
	}
	d, err := f.Dot()
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Perf Hawk" || d.Slug != "perf-hawk" || d.Delivery.Mode != dots.ModeInject || d.Trigger.OnTurn || d.Purpose != "Find slow code." {
		t.Fatalf("dot %+v", d)
	}
	if !reflect.DeepEqual(d.Trigger.FileGlobs, []string{"a.go", "b/**"}) {
		t.Fatalf("globs %v", d.Trigger.FileGlobs)
	}
}

func TestFormRejectsNonNumbers(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	focusOn(f, "Context turns")
	f.Update(k("backspace"))
	f.Update(k("z"))
	if _, err := f.Dot(); err == nil || !strings.Contains(err.Error(), "Context turns") {
		t.Fatalf("want number error, got %v", err)
	}
}
