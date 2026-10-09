// Package manage is the `prefix+d` popup: the dot library, templates, the dot
// editor, and attaching/detaching dots to the focused agent pane.
package manage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type Deps struct {
	Caller  Caller
	DotsDir string
	Check   func(ctx context.Context, kind string) error // backend CLI present?
	NewDot  func() dots.Dot                              // blank dot with config defaults; nil → dots.New("")
}

type SnapshotMsg view.Snapshot

type (
	doneMsg    struct{}
	errMsg     struct{ err error }
	formErrMsg struct{ err error }
	savedMsg   struct {
		slug  string
		isNew bool
	}
	deletedMsg  struct{ name string }
	reloadedMsg struct{}
)

type screen int

const (
	scrLibrary screen = iota
	scrTemplates
	scrEditor
	scrConfirm
)

type Model struct {
	d         Deps
	target    string
	snap      view.Snapshot
	loaded    bool
	scr       screen
	cursor    int
	tcursor   int
	selected  map[string]bool
	attached  map[string]bool
	templates []dots.Dot
	form      *Form
	status    string
	saving    bool
	delSlug   string // dot the confirm screen asks about, fixed when D is pressed
	delName   string
	width     int
}

func New(target string, d Deps) Model {
	return Model{d: d, target: target, selected: map[string]bool{}, attached: map[string]bool{}, templates: dots.Templates()}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) call(cmd string, args any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return m.d.Caller.Call(ctx, cmd, args)
}

func (m Model) reload() tea.Cmd {
	return func() tea.Msg {
		if err := m.call(ipc.CmdReloadDots, nil); err != nil {
			return errMsg{err}
		}
		return reloadedMsg{}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case SnapshotMsg:
		m.snap = view.Snapshot(msg)
		m.attached = map[string]bool{}
		if p, ok := m.snap.Pane(m.target); ok {
			for _, d := range p.Dots {
				m.attached[d.Slug] = true
			}
		}
		if !m.loaded {
			for s := range m.attached {
				m.selected[s] = true
			}
			m.loaded = true
		}
		m.cursor = max(0, min(m.cursor, len(m.snap.Library)-1))
	case errMsg:
		m.status = msg.err.Error()
	case formErrMsg:
		m.saving = false
		if m.form != nil {
			m.form.SetError(msg.err.Error())
		}
	case savedMsg:
		m.scr, m.form, m.saving = scrLibrary, nil, false
		m.status = "saved " + msg.slug
		if m.target != "" && msg.isNew {
			// Pre-check a dot just created here; an edited dot keeps its state,
			// so ⏎ never attaches something the user only edited.
			m.selected[msg.slug] = true
		}
		return m, m.reload()
	case deletedMsg:
		m.scr = scrLibrary
		m.status = "deleted " + msg.name
		return m, m.reload()
	case reloadedMsg:
	case doneMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		switch m.scr {
		case scrTemplates:
			return m.keysTemplates(msg)
		case scrEditor:
			return m.keysEditor(msg)
		case scrConfirm:
			return m.keysConfirm(msg)
		}
		return m.keysLibrary(msg)
	}
	return m, nil
}

func (m Model) current() (view.LibraryDot, bool) {
	if m.cursor >= 0 && m.cursor < len(m.snap.Library) {
		return m.snap.Library[m.cursor], true
	}
	return view.LibraryDot{}, false
}

func (m Model) keysLibrary(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch k.String() {
	case "esc", "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.snap.Library)-1))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case " ":
		if m.target == "" {
			m.status = "focus an agent pane to attach dots"
			return m, nil
		}
		if d, ok := m.current(); ok {
			m.selected[d.Slug] = !m.selected[d.Slug]
		}
	case "enter":
		if m.target == "" {
			return m, tea.Quit
		}
		return m, m.apply()
	case "n":
		blank := dots.New("")
		if m.d.NewDot != nil {
			blank = m.d.NewDot()
		}
		m.form, m.scr = NewForm(blank, true), scrEditor
	case "t":
		m.scr, m.tcursor = scrTemplates, 0
	case "e":
		if d, ok := m.current(); ok {
			full, err := dots.Load(m.d.DotsDir, d.Slug)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.form, m.scr = NewForm(full, false), scrEditor
		}
	case "D":
		if d, ok := m.current(); ok {
			m.scr, m.delSlug, m.delName = scrConfirm, d.Slug, d.Name
		}
	}
	return m, nil
}

// apply attaches newly checked dots in one call and detaches unchecked ones.
func (m Model) apply() tea.Cmd {
	var add []string
	var remove []string
	for _, d := range m.snap.Library {
		switch {
		case m.selected[d.Slug] && !m.attached[d.Slug]:
			add = append(add, d.Slug)
		case !m.selected[d.Slug] && m.attached[d.Slug]:
			remove = append(remove, d.Slug)
		}
	}
	return func() tea.Msg {
		if len(add) > 0 {
			if err := m.call(ipc.CmdAttach, ipc.AttachArgs{PaneID: m.target, Slugs: add}); err != nil {
				return errMsg{err}
			}
		}
		for _, s := range remove {
			if err := m.call(ipc.CmdDetach, ipc.DotArgs{PaneID: m.target, Slug: s}); err != nil {
				return errMsg{err}
			}
		}
		return doneMsg{}
	}
}

func (m Model) keysTemplates(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.scr = scrLibrary
	case "j", "down":
		m.tcursor = max(0, min(m.tcursor+1, len(m.templates)-1))
	case "k", "up":
		m.tcursor = max(m.tcursor-1, 0)
	case "enter":
		if m.tcursor >= 0 && m.tcursor < len(m.templates) {
			m.form, m.scr = NewForm(m.templates[m.tcursor], true), scrEditor
		}
	}
	return m, nil
}

func (m Model) keysEditor(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.form, m.scr, m.saving = nil, scrLibrary, false
		return m, nil
	case "ctrl+s":
		if m.saving {
			return m, nil
		}
		// Read the form synchronously: the returned cmd runs on another
		// goroutine and must only see the immutable value.
		d, err := m.form.Dot()
		if err != nil {
			m.form.SetError(err.Error())
			return m, nil
		}
		m.form.SetError("")
		m.saving = true
		return m, m.save(d, m.form.IsNew())
	}
	if m.saving {
		return m, nil
	}
	return m, m.form.Update(k)
}

func (m Model) save(d dots.Dot, isNew bool) tea.Cmd {
	dir, check := m.d.DotsDir, m.d.Check
	return func() tea.Msg {
		if err := d.Validate(); err != nil {
			return formErrMsg{err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := check(ctx, d.Backend.Kind); err != nil {
			return formErrMsg{err}
		}
		if err := dots.Save(dir, d, isNew); err != nil {
			if errors.Is(err, dots.ErrExists) {
				return formErrMsg{fmt.Errorf("a dot named %q already exists: pick another name", d.Name)}
			}
			return formErrMsg{err}
		}
		slug := d.Slug
		if slug == "" {
			slug = dots.Slugify(d.Name)
		}
		return savedMsg{slug: slug, isNew: isNew}
	}
}

func (m Model) keysConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		dir, slug, name := m.d.DotsDir, m.delSlug, m.delName
		m.scr = scrLibrary
		return m, func() tea.Msg {
			if err := dots.Delete(dir, slug); err != nil {
				return errMsg{err}
			}
			return deletedMsg{name: name}
		}
	default:
		m.scr = scrLibrary
	}
	return m, nil
}

func (m Model) View() string {
	switch m.scr {
	case scrEditor:
		v := m.form.View(m.width)
		if m.saving {
			v += "\n" + style.Dim.Render("saving…")
		}
		return v
	case scrTemplates:
		var b strings.Builder
		b.WriteString(style.Title.Render("NEW DOT FROM TEMPLATE") + "\n\n")
		for i, d := range m.templates {
			marker := "  "
			if i == m.tcursor {
				marker = "▸ "
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", marker, d.Glyph, d.Name))
		}
		b.WriteString("\n" + style.Dim.Render("j/k move · enter use · esc back"))
		return b.String()
	case scrConfirm:
		return style.Title.Render(fmt.Sprintf("Delete %s?", m.delName)) + "\n\n" + style.Dim.Render("y delete · any other key cancels")
	}
	var b strings.Builder
	head := "DOTS LIBRARY ─ target: " + m.target
	if m.target == "" {
		head = "DOTS LIBRARY ─ no agent pane focused (library only)"
	}
	b.WriteString(style.Title.Render(head) + "\n\n")
	if len(m.snap.Library) == 0 {
		b.WriteString(style.Dim.Render("No dots yet. Press t to start from a template or n for a new dot.") + "\n")
	}
	for i, d := range m.snap.Library {
		check := "[ ]"
		if m.selected[d.Slug] {
			check = "[x]"
		}
		line := fmt.Sprintf("%s %s %-14s %-22s %s", check, style.Glyph(d.Glyph, d.Color), d.Name, d.Backend+"/"+d.Model, d.Mode)
		if i == m.cursor {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for _, inv := range m.snap.Invalid {
		b.WriteString(style.Err.Render(fmt.Sprintf("⚠ %s:%d: %s", inv.File, inv.Line, inv.Error)) + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("space toggle · enter apply · n new · t template · e edit · D delete · esc close"))
	return b.String()
}
