package keys

import (
	"reflect"
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

// cmdToy is toy whose keys return commands that log them: s opens a text
// field, anything else is a browse command (an IPC call, a screen that opens
// asynchronously).
func cmdToy(order *[]string) func(toy, tea.KeyMsg) (toy, tea.Cmd) {
	return func(t toy, k tea.KeyMsg) (toy, tea.Cmd) {
		s := k.String()
		t.got = append(t.got, s)
		if t.mode == Browse && s == "s" {
			t.mode = Text
		}
		return t, func() tea.Msg { *order = append(*order, s); return nil }
	}
}

// I1: a key whose command may open a screen later (asynchronously) ends the
// burst: the rest would land on the old screen as commands.
func TestABurstStopsAtTheFirstCommand(t *testing.T) {
	var order []string
	m, _ := Deliver(toy{}, runes("Py"), modeOf, cmdToy(&order))
	if len(m.got) != 1 || m.got[0] != "P" {
		t.Fatalf("keys %q; want P alone, the rest dropped", m.got)
	}
}

// M1: commands run, and their messages arrive, in typed order.
func TestCommandsRunInTypedOrder(t *testing.T) {
	var order []string
	m, cmd := Deliver(toy{}, runes("shi"), modeOf, cmdToy(&order))
	if len(m.got) != 2 || m.got[1] != "hi" {
		t.Fatalf("keys %q; want s, then hi typed", m.got)
	}
	if _, batch := cmd().(tea.BatchMsg); batch {
		t.Fatal("a tea.BatchMsg runs its commands concurrently: their messages could arrive out of order")
	}
	order = nil
	run(cmd)
	if len(order) != 2 || order[0] != "s" || order[1] != "hi" {
		t.Fatalf("order %q", order)
	}
}

func TestNoCommandsMeansNoCommand(t *testing.T) {
	if _, cmd := Deliver(toy{}, runes("jj"), modeOf, toy.step); cmd != nil {
		t.Fatal("want a nil command when no key returned one")
	}
}

// run executes cmd the way the runtime does, in order through a sequence
// (tea.Sequence's message is an unexported slice of tea.Cmd).
func run(cmd tea.Cmd) {
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeOf(tea.Cmd(nil)) {
		return
	}
	for i := 0; i < v.Len(); i++ {
		if c := v.Index(i).Interface().(tea.Cmd); c != nil {
			run(c)
		}
	}
}
