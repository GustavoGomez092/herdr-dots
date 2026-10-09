package manage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/ui/style"
)

// KeyStore is the credentials store the Providers screen edits
// (credentials.File in production).
type KeyStore interface {
	List() ([]credentials.Status, error)
	Set(provider, key string) error
	Remove(provider string) error
}

// providers is the Providers screen (K from the library). The full key is
// never rendered: input is masked and rows show only the last 4 characters.
type providers struct {
	rows     []credentials.Status
	cursor   int
	entering bool
	enterFor string // provider the key being typed belongs to, fixed when s is pressed
	input    textinput.Model
	status   string
	tested   map[string]string // provider → "✓ verified 15:04" or the redacted error
}

type (
	providersMsg struct {
		rows []credentials.Status
		err  error
	}
	keyStoredMsg struct {
		provider string
		err      error
	}
	keyTestedMsg struct {
		provider string
		err      error
	}
	refreshedMsg struct { // after a store: the new list and the test result
		list providersMsg
		test keyTestedMsg
	}
)

func newProviders() *providers {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 512
	ti.Placeholder = "paste the API key"
	return &providers{input: ti, tested: map[string]string{}}
}

// loadProviders lists key sources, plus an ollama row (no key).
func (m Model) loadProviders() tea.Cmd {
	store := m.d.Keys
	return func() tea.Msg {
		var rows []credentials.Status
		var err error
		if store != nil {
			rows, err = store.List()
		}
		rows = append(rows, credentials.Status{Provider: "ollama", From: "no key"})
		return providersMsg{rows: rows, err: err}
	}
}

func (m Model) testProvider(provider string) tea.Cmd {
	test := m.d.TestKey
	return func() tea.Msg {
		if test == nil {
			return keyTestedMsg{provider: provider, err: fmt.Errorf("testing is unavailable here")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return keyTestedMsg{provider: provider, err: test(ctx, provider)}
	}
}

func (m Model) providersUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := m.prov
	switch msg := msg.(type) {
	case providersMsg:
		// Keep the cursor on the same provider across reloads.
		cur := ""
		if p.cursor >= 0 && p.cursor < len(p.rows) {
			cur = p.rows[p.cursor].Provider
		}
		p.rows = msg.rows
		p.cursor = max(0, min(p.cursor, len(p.rows)-1))
		for i, r := range p.rows {
			if cur != "" && r.Provider == cur {
				p.cursor = i
			}
		}
		if msg.err != nil {
			p.status = credentials.Redact(msg.err.Error())
		}
		return m, nil
	case keyStoredMsg:
		if msg.err != nil {
			p.status = credentials.Redact(msg.err.Error())
			return m, nil
		}
		p.status = "stored " + msg.provider + " · testing…"
		load, test := m.loadProviders(), m.testProvider(msg.provider)
		return m, func() tea.Msg {
			return refreshedMsg{list: load().(providersMsg), test: test().(keyTestedMsg)}
		}
	case refreshedMsg:
		m2, _ := m.providersUpdate(msg.list)
		return m2.(Model).providersUpdate(msg.test)
	case keyTestedMsg:
		if msg.err != nil {
			p.tested[msg.provider] = "✗ " + credentials.Redact(msg.err.Error())
		} else {
			p.tested[msg.provider] = "✓ verified " + time.Now().Format("15:04")
		}
		p.status = ""
		return m, nil
	case tea.KeyMsg:
		return m.providersKeys(msg)
	}
	return m, nil
}

func (m Model) providersKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.prov
	if p.entering {
		switch k.String() {
		case "esc":
			p.entering = false
			p.input.Reset()
			p.input.Blur()
			return m, nil
		case "enter":
			key := strings.TrimSpace(p.input.Value())
			provider, store := p.enterFor, m.d.Keys
			p.input.Reset()
			p.input.Blur()
			p.entering = false
			if key == "" || store == nil || provider == "" {
				return m, nil
			}
			return m, func() tea.Msg { return keyStoredMsg{provider: provider, err: store.Set(provider, key)} }
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(k)
		return m, cmd
	}
	cur := ""
	if p.cursor >= 0 && p.cursor < len(p.rows) {
		cur = p.rows[p.cursor].Provider
	}
	switch k.String() {
	case "esc", "q":
		m.scr = scrLibrary
	case "j", "down":
		p.cursor = min(p.cursor+1, max(0, len(p.rows)-1))
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
	case "s":
		if cur == "" || cur == "ollama" {
			p.status = "ollama needs no key"
			return m, nil
		}
		p.entering, p.enterFor, p.status = true, cur, ""
		p.input.Focus()
	case "t":
		if cur != "" {
			p.status = "testing " + cur + "…"
			return m, m.testProvider(cur)
		}
	case "x":
		if cur == "" || cur == "ollama" || m.d.Keys == nil {
			return m, nil
		}
		store, load := m.d.Keys, m.loadProviders()
		return m, func() tea.Msg {
			if err := store.Remove(cur); err != nil {
				return keyStoredMsg{provider: cur, err: err} // shows the redacted error, keeps the rows
			}
			return load()
		}
	}
	return m, nil
}

func (m Model) providersView() string {
	p := m.prov
	var b strings.Builder
	b.WriteString(style.Title.Render("PROVIDERS") + "\n\n")
	for i, r := range p.rows {
		src := r.From
		if r.Provider == "ollama" {
			src = m.d.OllamaURL
			if src == "" {
				src = "http://127.0.0.1:11434"
			}
		}
		line := fmt.Sprintf("%-11s %-24s %-6s %s", r.Provider, src, r.Last4, p.tested[r.Provider])
		if i == p.cursor {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if p.entering {
		b.WriteString("\nkey for " + p.enterFor + ": " + p.input.View() + "\n")
		b.WriteString(style.Dim.Render("enter store · esc cancel · stored in credentials.toml (0600)") + "\n")
	}
	if p.status != "" {
		b.WriteString("\n" + p.status + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("s set key · t test · x remove · esc back"))
	return b.String()
}
