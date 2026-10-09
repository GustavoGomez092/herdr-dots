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

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/memui"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type Deps struct {
	MCPStatus func() string // footer line about the MCP server install (Phase 5); nil = none

	Caller  Caller
	DotsDir string
	Check   func(ctx context.Context, kind string) error // backend CLI present?
	NewDot  func() dots.Dot                              // blank dot with config defaults; nil → dots.New("")

	Memory  *memory.Store                               // nil: memory off (no M/P, no memory step on delete)
	Project func(target string) (memory.Project, error) // the target pane's project when the snapshot lacks it; may be nil

	// Phase 4 (all optional).
	CheckDot  func(ctx context.Context, d dots.Dot) (warn string, err error)    // runs on save instead of Check: key + model; warn = saved unchecked
	Models    func(ctx context.Context, kind, baseURL string) ([]string, error) // ctrl+space model picker (the dot's base_url, if any)
	Estimate  func(d dots.Dot) string                                           // editor cost line
	Keys      KeyStore                                                          // Providers screen
	TestKey   func(ctx context.Context, provider string) error                  // Providers screen "t"
	OllamaURL string                                                            // shown on the ollama row
}

type SnapshotMsg view.Snapshot

type (
	doneMsg    struct{}
	errMsg     struct{ err error }
	formErrMsg struct{ err error }
	savedMsg   struct {
		slug  string
		isNew bool
		warn  string // the save check couldn't reach the provider
	}
	deletedMsg  struct{ name, slug string }
	reloadedMsg struct{}
	memOpenMsg  struct {
		deps memui.Deps
		err  error
	}
	memDeletedMsg struct {
		name string
		del  memory.DotDeletion
	}
	modelsMsg struct {
		kind string
		ids  []string
		err  error
	}
)

type screen int

const (
	scrLibrary screen = iota
	scrTemplates
	scrEditor
	scrConfirm
	scrMemory        // the memory browser (memui)
	scrConfirmMemory // after deleting a dot: also delete its memory?
	scrProviders     // API keys and the Ollama server (Phase 4)
)

type Model struct {
	mcpStatus string // Deps.MCPStatus, read once when the popup opens

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
	height    int
	mem       *memui.Model // open memory browser (scrMemory)
	prov      *providers
	fetching  bool // a ctrl+space model fetch is in flight
}

func New(target string, d Deps) Model {
	m := Model{d: d, target: target, selected: map[string]bool{}, attached: map[string]bool{}, templates: dots.Templates(), prov: newProviders()}
	if d.MCPStatus != nil {
		m.mcpStatus = d.MCPStatus()
	}
	return m
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
	if m.scr == scrMemory && m.mem != nil {
		switch msg.(type) {
		case SnapshotMsg: // the library keeps tracking snapshots below
		case memui.BackMsg:
			m.scr, m.mem = scrLibrary, nil
			return m, nil
		default:
			if ws, ok := msg.(tea.WindowSizeMsg); ok {
				m.width, m.height = ws.Width, ws.Height
			}
			nm, cmd := m.mem.Update(msg)
			mm := nm.(memui.Model)
			m.mem = &mm
			return m, cmd
		}
	}
	switch msg := msg.(type) {
	case memOpenMsg:
		if msg.deps.Store == nil {
			if msg.err != nil {
				m.status = msg.err.Error()
			}
			return m, nil
		}
		mm := memui.New(msg.deps)
		m.mem, m.scr = &mm, scrMemory
		if m.width > 0 || m.height > 0 {
			nm, _ := mm.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			mm = nm.(memui.Model)
			m.mem = &mm
		}
		return m, mm.Init()
	case memDeletedMsg:
		m.status = fmt.Sprintf("deleted %s and its memory (%s)", msg.name, msg.del)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		if msg.warn != "" {
			m.status += " · " + msg.warn
		}
		if m.target != "" && msg.isNew {
			// Pre-check a dot just created here; an edited dot keeps its state,
			// so ⏎ never attaches something the user only edited.
			m.selected[msg.slug] = true
		}
		return m, m.reload()
	case deletedMsg:
		m.scr = scrLibrary
		m.status = "deleted " + msg.name
		if m.d.Memory != nil {
			m.scr, m.delSlug, m.delName = scrConfirmMemory, msg.slug, msg.name
		}
		return m, m.reload()
	case reloadedMsg:
	case providersMsg, keyStoredMsg, keyTestedMsg, refreshedMsg:
		return m.providersUpdate(msg)
	case modelsMsg:
		m.fetching = false
		if m.form != nil && msg.kind == m.form.Kind() {
			if msg.err != nil {
				m.form.SetError(credentials.Redact(msg.err.Error()))
			} else {
				m.form.SetModels(msg.kind, msg.ids)
			}
		}
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
		case scrConfirmMemory:
			return m.keysConfirmMemory(msg)
		case scrProviders:
			return m.providersUpdate(msg)
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
		m.form, m.scr = m.newForm(blank, true), scrEditor
	case "t":
		m.scr, m.tcursor = scrTemplates, 0
	case "K":
		m.scr = scrProviders
		return m, m.loadProviders()
	case "e":
		if d, ok := m.current(); ok {
			full, err := dots.Load(m.d.DotsDir, d.Slug)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.form, m.scr = m.newForm(full, false), scrEditor
		}
	case "D":
		if d, ok := m.current(); ok {
			m.scr, m.delSlug, m.delName = scrConfirm, d.Slug, d.Name
		}
	case "M":
		if m.d.Memory == nil {
			m.status = "memory is turned off (memory_enabled = false)"
			return m, nil
		}
		if d, ok := m.current(); ok {
			return m, m.openMemory(d.Slug, d.Name)
		}
	case "P":
		if m.d.Memory == nil {
			m.status = "memory is turned off (memory_enabled = false)"
			return m, nil
		}
		return m, m.openMemory("", "")
	}
	return m, nil
}

// openMemory resolves the target pane's project (snapshot first, then
// Deps.Project) off the UI goroutine and opens the memory browser.
func (m Model) openMemory(dot, name string) tea.Cmd {
	pv, _ := m.snap.Pane(m.target)
	target, resolve, st, caller := m.target, m.d.Project, m.d.Memory, m.d.Caller
	return func() tea.Msg {
		pr := memory.Project{ID: pv.ProjectID, Name: pv.ProjectName}
		var err error
		if pr.ID == "" && target != "" && resolve != nil {
			pr, err = resolve(target)
		}
		if pr.ID == "" && dot == "" {
			if err == nil {
				err = errors.New("no project: focus an agent pane first")
			}
			return memOpenMsg{err: err}
		}
		notice := ""
		switch {
		case err != nil:
			notice = "project memory unavailable: " + err.Error()
		case pr.ID == "":
			notice = "no project found; showing this dot's global memory"
		}
		notify := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = caller.Call(ctx, ipc.CmdMemoryChanged, nil)
		}
		return memOpenMsg{deps: memui.Deps{Store: st, Project: pr, Dot: dot, DotName: name, Notify: notify, Notice: notice}}
	}
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
			m.form, m.scr = m.newForm(m.templates[m.tcursor], true), scrEditor
		}
	}
	return m, nil
}

func (m Model) newForm(d dots.Dot, isNew bool) *Form {
	f := NewForm(d, isNew)
	f.SetEstimator(m.d.Estimate)
	return f
}

// fetchModels loads ctrl+space choices for kind from the dot's base_url (if
// it sets one).
func (m Model) fetchModels(kind, baseURL string) tea.Cmd {
	list := m.d.Models
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ids, err := list(ctx, kind, baseURL)
		return modelsMsg{kind: kind, ids: ids, err: err}
	}
}

func (m Model) keysEditor(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+@", "ctrl+ ":
		if m.form.OnModel() && !m.saving && !m.fetching && !m.form.NextModel() && m.d.Models != nil {
			m.fetching = true
			return m, m.fetchModels(m.form.Kind(), m.form.BaseURL())
		}
		return m, nil
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
	dir, check, checkDot := m.d.DotsDir, m.d.Check, m.d.CheckDot
	return func() tea.Msg {
		if err := d.Validate(); err != nil {
			return formErrMsg{err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		warn := ""
		if checkDot != nil {
			w, err := checkDot(ctx, d)
			if err != nil {
				return formErrMsg{errors.New(credentials.Redact(err.Error()))}
			}
			warn = credentials.Redact(w)
		} else if err := check(ctx, d.Backend.Kind); err != nil {
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
		return savedMsg{slug: slug, isNew: isNew, warn: warn}
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
			return deletedMsg{name: name, slug: slug}
		}
	default:
		m.scr = scrLibrary
	}
	return m, nil
}

// keysConfirmMemory answers "Also delete its memory in all projects? (Y/n)".
func (m Model) keysConfirmMemory(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	slug, name, st, caller := m.delSlug, m.delName, m.d.Memory, m.d.Caller
	switch k.String() {
	case "n", "N", "esc":
		m.scr = scrLibrary
		m.status = "deleted " + name + " (memory kept)"
		return m, nil
	case "y", "Y", "enter":
		m.scr = scrLibrary
		return m, func() tea.Msg {
			del, err := st.DeleteDot(slug)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = caller.Call(ctx, ipc.CmdMemoryChanged, nil) // part of it may be gone even on an error
			if err != nil {
				return errMsg{err}
			}
			return memDeletedMsg{name: name, del: del}
		}
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
	case scrConfirmMemory:
		return style.Title.Render(fmt.Sprintf("Deleted %s.", m.delName)) + "\nAlso delete its memory in all projects? (Y/n)\n\n" +
			style.Dim.Render("y/enter delete memory · n keep it (re-creating the dot picks it back up)")
	case scrMemory:
		if m.mem != nil {
			return m.mem.View()
		}
	case scrProviders:
		return m.providersView()
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
	if m.mcpStatus != "" {
		b.WriteString("\n" + style.Dim.Render(m.mcpStatus) + "\n")
	}
	help := "space toggle · enter apply · n new · t template · e edit · D delete · K providers · esc close"
	if m.d.Memory != nil {
		help += " · M memory · P project memory"
	}
	b.WriteString("\n" + style.Dim.Render(help))
	return b.String()
}
