// Package keys hands a burst of typed keys to a Dots TUI one key at a time.
//
// Bubble Tea (v1.3) merges printable characters that arrive in one read into
// a single KeyMsg with several Runes ("jjj"), and the TUIs match keys by
// KeyMsg.String(), so a fast "jj" matched nothing and was dropped (the Linux
// interactive check's B1). Deliver splits such a burst into one key per rune
// while the model is browsing, and never lets a later rune of a burst answer
// a confirm an earlier rune opened: a confirm takes a separately delivered
// key.
package keys

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Mode is what a model does with a burst of typed keys right now.
type Mode int

const (
	// Browse: every key is a command, so a burst is one key per rune.
	Browse Mode = iota
	// Pager: a review screen whose approval (y, x...) must have its own
	// key. A burst is split, but only its scroll runes (pagerRunes) act:
	// the first other rune and everything after it are dropped.
	Pager
	// Text: a text field has focus, so the burst is typed as it came
	// (pasted or fast-typed text is never turned into commands).
	Text
	// Modal: a confirm or a chooser. A burst arriving while it is open is
	// handed on as it came, which matches no key; one arriving after an
	// earlier rune of the same burst opened it is dropped.
	Modal
)

// pagerRunes are the scroll keys every Dots pager shares.
const pagerRunes = "jkbg"

// Burst reports whether k is several printable keys merged into one
// message. A bracketed paste and an alt key are never a burst.
func Burst(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && len(k.Runes) > 1 && !k.Paste && !k.Alt
}

// Deliver hands k to step: as it came unless it is a burst arriving in
// Browse or Pager mode, which step gets one rune at a time. After each rune
// the model's mode decides what happens to the rest: Browse goes on, Pager
// goes on with scroll runes only, Text gets the rest as one message (typed
// into the field the rune opened) and Modal drops it. The commands run in
// the order the keys were typed.
func Deliver[M any](m M, k tea.KeyMsg, mode func(M) Mode, step func(M, tea.KeyMsg) (M, tea.Cmd)) (M, tea.Cmd) {
	if !Burst(k) {
		return step(m, k)
	}
	if md := mode(m); md != Browse && md != Pager {
		return step(m, k)
	}
	var cmds []tea.Cmd
	for i, r := range k.Runes {
		var cmd tea.Cmd
		switch mode(m) {
		case Text:
			rest := tea.KeyMsg{Type: tea.KeyRunes, Runes: append([]rune(nil), k.Runes[i:]...)}
			m, cmd = step(m, rest)
			return m, ordered(append(cmds, cmd))
		case Modal:
			return m, ordered(cmds)
		case Pager:
			if !strings.ContainsRune(pagerRunes, r) {
				return m, ordered(cmds)
			}
		}
		m, cmd = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		cmds = append(cmds, cmd)
	}
	return m, ordered(cmds)
}

// ordered is one command that runs the non-nil cmds one after another, so
// their effects (IPC calls) happen in the order the keys were typed, and
// hands their messages back as a tea.BatchMsg. It is nil for no command and
// the command itself for one.
func ordered(cmds []tea.Cmd) tea.Cmd {
	var live []tea.Cmd
	for _, c := range cmds {
		if c != nil {
			live = append(live, c)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return func() tea.Msg {
		var out tea.BatchMsg
		for _, c := range live {
			if msg := c(); msg != nil {
				out = append(out, func() tea.Msg { return msg })
			}
		}
		return out
	}
}
