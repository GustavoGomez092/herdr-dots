package keys

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// toy is a model with a browse mode (j moves, x opens a confirm, s opens a
// text field, v opens a pager) and records every key it is handed.
type toy struct {
	mode   Mode
	cursor int
	got    []string
	text   string
}

func (t toy) step(k tea.KeyMsg) (toy, tea.Cmd) {
	t.got = append(t.got, k.String())
	switch t.mode {
	case Text:
		t.text += string(k.Runes)
		return t, nil
	case Modal:
		return t, nil
	}
	switch k.String() {
	case "j":
		t.cursor++
	case "x":
		t.mode = Modal
	case "s":
		t.mode = Text
	case "v":
		t.mode = Pager
	}
	return t, nil
}

func modeOf(t toy) Mode { return t.mode }

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestABurstInBrowseModeIsOneKeyPerRune(t *testing.T) {
	m, _ := Deliver(toy{}, runes("jjj"), modeOf, toy.step)
	if m.cursor != 3 || len(m.got) != 3 {
		t.Fatalf("cursor %d, keys %q; want 3 single keys", m.cursor, m.got)
	}
}

func TestABurstNeverConfirmsWhatAnEarlierRuneOpened(t *testing.T) {
	m, _ := Deliver(toy{}, runes("xy"), modeOf, toy.step)
	if m.mode != Modal || len(m.got) != 1 || m.got[0] != "x" {
		t.Fatalf("mode %v, keys %q; want x alone, y dropped", m.mode, m.got)
	}
}

func TestTheRestOfABurstIsTypedIntoAFieldItOpened(t *testing.T) {
	m, _ := Deliver(toy{}, runes("shello"), modeOf, toy.step)
	if m.text != "hello" {
		t.Fatalf("text %q, keys %q; want hello", m.text, m.got)
	}
}

func TestATextFieldGetsTheBurstUnchanged(t *testing.T) {
	m, _ := Deliver(toy{mode: Text}, runes("hello"), modeOf, toy.step)
	if m.text != "hello" || len(m.got) != 1 {
		t.Fatalf("text %q, keys %q; want one message", m.text, m.got)
	}
}

func TestAModalOpenFirstGetsTheBurstUnchanged(t *testing.T) {
	m, _ := Deliver(toy{mode: Modal}, runes("yy"), modeOf, toy.step)
	if len(m.got) != 1 || m.got[0] != "yy" {
		t.Fatalf("keys %q; want the burst as one message", m.got)
	}
}

func TestAPagerScrollsButNeverTakesAnApprovalFromABurst(t *testing.T) {
	m, _ := Deliver(toy{mode: Pager}, runes("jjyj"), modeOf, toy.step)
	if len(m.got) != 2 || m.got[0] != "j" || m.got[1] != "j" {
		t.Fatalf("keys %q; want j j, then y and the rest dropped", m.got)
	}
}

func TestPastesAndAltKeysAreNeverSplit(t *testing.T) {
	paste := runes("jjj")
	paste.Paste = true
	if m, _ := Deliver(toy{}, paste, modeOf, toy.step); m.cursor != 0 || len(m.got) != 1 {
		t.Fatalf("paste split: %q", m.got)
	}
	alt := runes("jj")
	alt.Alt = true
	if m, _ := Deliver(toy{}, alt, modeOf, toy.step); m.cursor != 0 || len(m.got) != 1 {
		t.Fatalf("alt split: %q", m.got)
	}
}

func TestASingleKeyIsHandedOnAsIs(t *testing.T) {
	m, _ := Deliver(toy{}, runes("j"), modeOf, toy.step)
	if m.cursor != 1 || len(m.got) != 1 {
		t.Fatalf("cursor %d, keys %q", m.cursor, m.got)
	}
}

func TestCommandsRunInTypedOrder(t *testing.T) {
	var order []string
	step := func(t toy, k tea.KeyMsg) (toy, tea.Cmd) {
		s := k.String()
		return t, func() tea.Msg { order = append(order, s); return nil }
	}
	_, cmd := Deliver(toy{}, runes("abc"), modeOf, step)
	if cmd == nil {
		t.Fatal("no command")
	}
	run(cmd)
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("order %q", order)
	}
}

func TestNoCommandsMeansNoCommand(t *testing.T) {
	if _, cmd := Deliver(toy{}, runes("jj"), modeOf, toy.step); cmd != nil {
		t.Fatal("want a nil command when no key returned one")
	}
}

// run executes cmd the way the runtime does, through any batch.
func run(cmd tea.Cmd) {
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				run(c)
			}
		}
	}
}
