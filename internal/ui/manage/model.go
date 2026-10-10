// Package manage is the `prefix+d` popup: the dot library, templates, the dot
// editor, and attaching/detaching dots to the focused agent pane.
package manage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/memui"
	"herdr-dots/internal/ui/profile"
	setupui "herdr-dots/internal/ui/setup"
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

	// Phase 9: profiles and sharing.
	History    *history.Store                                                // nil: no profiles
	Fetch      func(ctx context.Context, url string) ([]byte, string, error) // dot pack downloads; nil: files only
	SessionKey string                                                        // this Herdr session (joins history rows to queued cards)
	ExportDir  string                                                        // default folder for exports; "" = home

	// Phase 4 (all optional).
	CheckDot  func(ctx context.Context, d dots.Dot) (warn string, err error)    // runs on save instead of Check: key + model; warn = saved unchecked
	Models    func(ctx context.Context, kind, baseURL string) ([]string, error) // ctrl+space model picker (the dot's base_url, if any)
	Estimate  func(d dots.Dot) string                                           // editor cost line
	Keys      KeyStore                                                          // Providers screen
	TestKey   func(ctx context.Context, provider string) error                  // Providers screen "t"
	OllamaURL string                                                            // shown on the ollama row

	// Post-rc: the Setup screen (MCP server + keybindings, setup.go).
	// Setup.FirstRun runs from Init; Setup.Check nil: no S.
	Setup setupui.Deps
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
	scrProfile       // a dot's profile (Phase 9)
	scrImportSource  // import: file path or URL prompt
	scrImportPreview // import: everything the pack would do
	scrImportRename  // import: a new name after a collision
	scrExport        // export: memory choice and file
	scrTrust         // a project dot's trust preview
	scrSetup         // the Setup screen: first run, or S (post-rc)
)

type Model struct {
	mcpStatus   string // Deps.MCPStatus, read when the popup opens and after the Setup screen
	onboardNote string // a damaged onboarding.json (setup.go)

	d         Deps
	target    string
	snap      view.Snapshot
	scr       screen
	cursor    int
	tcursor   int
	selected  map[string]bool
	touched   map[string]bool // boxes the developer set here; the rest follow the pane
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

	prof  *profile.Model  // scrProfile (kept while its memory browser or export is open)
	imp   *importState    // scrImportPreview, scrImportRename
	exp   *exportState    // scrExport
	trust *trustState     // scrTrust
	input textinput.Model // the import, rename and export prompts

	setup *setupState // scrSetup
}

func New(target string, d Deps) Model {
	m := Model{d: d, target: target, selected: map[string]bool{}, touched: map[string]bool{}, attached: map[string]bool{}, templates: dots.Templates(), prov: newProviders()}
	if d.MCPStatus != nil {
		m.mcpStatus = d.MCPStatus()
	}
	return m
}

// Init asks the engine to reload dots and scan the target pane's repository,
// so its project dots show up before anything is attached, and runs the
// Setup screen's first-run detection.
func (m Model) Init() tea.Cmd {
	var reload tea.Cmd
	if m.target != "" {
		reload = func() tea.Msg {
			if err := m.call(ipc.CmdReloadDots, ipc.ReloadArgs{PaneID: m.target}); err != nil {
				return errMsg{err}
			}
			return nil
		}
	}
	return tea.Batch(reload, m.detectSetup())
}

// library is what this popup lists: your dots, plus the project dots of the
// target pane's checkout (ruling P9-R13: never another repository's, and
// never another checkout's copy of the same repository).
func (m Model) library() []view.LibraryDot {
	root := m.snap.PaneRoots[m.target]
	var out []view.LibraryDot
	for _, d := range m.snap.Library {
		if d.Origin == dots.OriginProject && (root == "" || d.Root != root) {
			continue
		}
		out = append(out, d)
	}
	return out
}

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
	if mm, cmd, ok := m.setupMsg(msg); ok {
		return mm, cmd
	}
	if k, ok := msg.(tea.KeyMsg); ok && m.scr == scrSetup && m.setup != nil {
		return m.keysSetup(k)
	}
	if ws, ok := msg.(tea.WindowSizeMsg); ok && m.prof != nil && m.scr != scrProfile {
		// A profile kept under its export or memory browser stays sized.
		pm, _ := m.prof.Update(ws)
		m.prof = &pm
	}
	if m.scr == scrProfile && m.prof != nil {
		switch msg := msg.(type) {
		case SnapshotMsg: // the library keeps tracking snapshots below
		case errMsg: // handled below: the status line shows it under the profile
		case profile.BackMsg:
			m.scr, m.prof, m.status = scrLibrary, nil, ""
			return m, nil
		case profile.ExportMsg:
			return m.startExport(msg.Slug)
		case profile.MemoryMsg:
			return m, m.openMemory(msg.Slug, msg.Name)
		case profile.TrustMsg:
			if m.target == "" {
				m.status = "focus an agent pane in that repository to trust its dots"
				return m, nil
			}
			return m, m.openTrust(msg.Dot)
		case memOpenMsg: // handled below: the memory browser opens over the profile
		case trustOpenMsg: // handled below: the trust preview opens over the profile
		default:
			if ws, ok := msg.(tea.WindowSizeMsg); ok {
				m.width, m.height = ws.Width, ws.Height
			}
			if _, ok := msg.(tea.KeyMsg); ok {
				m.status = ""
			}
			pm, cmd := m.prof.Update(msg)
			m.prof = &pm
			return m, cmd
		}
	}
	if m.scr == scrMemory && m.mem != nil {
		switch msg.(type) {
		case SnapshotMsg: // the library keeps tracking snapshots below
		case memui.BackMsg:
			m.scr, m.mem = scrLibrary, nil
			if m.prof != nil {
				m.scr = scrProfile // opened from the profile: back to it
			}
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
	case profileOpenMsg:
		if msg.err != nil {
			m.status = style.Clean(msg.err.Error())
			return m, nil
		}
		pm := profile.New(msg.deps)
		if m.width > 0 || m.height > 0 {
			pm, _ = pm.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		}
		m.prof, m.scr, m.status = &pm, scrProfile, ""
		return m, pm.Init()
	case trustOpenMsg:
		if msg.err != nil {
			m.status = style.Clean(msg.err.Error())
			return m, nil
		}
		// Opened from a profile, the preview replaces it: done or cancelled,
		// it goes back to the library, and nothing returns to a stale profile.
		m.trust, m.scr, m.status, m.prof = msg.st, scrTrust, "", nil
		m.trust.pg.fit(len(m.trustLines()), m.bodyHeight(), m.measured())
		return m, nil
	case trustDoneMsg:
		m.scr, m.trust = scrLibrary, nil
		if msg.trusted {
			m.status = "trusted " + msg.name + "; any change to its file revokes it"
		} else {
			m.status = "untrusted " + msg.name + "; attached copies pause"
		}
		return m, nil
	case importLoadedMsg:
		if msg.err != nil {
			m.status = style.Clean(msg.err.Error())
			return m, nil
		}
		m.imp, m.scr, m.status = msg.st, scrImportPreview, ""
		m.imp.pg.fit(len(m.importLines()), m.bodyHeight(), m.measured())
		return m, nil
	case importedMsg:
		m.scr, m.imp = scrLibrary, nil
		m.status = fmt.Sprintf("imported %s as %s", style.Clean(msg.name), msg.slug)
		if n := msg.res.Added + msg.res.Updated; n > 0 || len(msg.res.Rejected) > 0 {
			m.status += fmt.Sprintf(" %s memory: %d proposals to approve (P), %d skipped", style.Sym("·"), n, len(msg.res.Rejected))
		}
		if msg.memErr != nil {
			m.status += " " + style.Sym("·") + " memory import failed: " + style.Clean(msg.memErr.Error())
		}
		if m.target != "" {
			// Like a new dot: pre-checked, so enter attaches it (clamped,
			// as the preview showed).
			m.selected[msg.slug], m.touched[msg.slug] = true, true
		}
		return m, m.reload()
	case exportedMsg:
		m.scr, m.exp = scrProfile, nil
		if m.prof == nil {
			m.scr = scrLibrary
		}
		m.status = fmt.Sprintf("exported to %s (%d memory entries)", shortHome(msg.path), msg.entries)
		if msg.dropped > 0 {
			m.status += fmt.Sprintf("; %d left out: they looked like secrets", msg.dropped)
		}
		return m, nil
	case exportSecretMsg:
		if m.exp != nil {
			m.exp.secrets, m.exp.dropSecrets = msg.where, false
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.trust != nil {
			m.trust.pg.fit(len(m.trustLines()), m.bodyHeight(), m.measured())
		}
		if m.imp != nil {
			m.imp.pg.fit(len(m.importLines()), m.bodyHeight(), m.measured())
		}
		if m.setup != nil {
			m.setup.pg.fit(len(m.setupLines()), m.bodyHeight(), m.measured())
		}
	case SnapshotMsg:
		m.snap = view.Snapshot(msg)
		if m.trust != nil {
			// The open preview keeps its bytes and hash; it learns whether
			// the engine now sees another file, or trusts this one.
			for _, d := range m.snap.Library {
				if d.Origin == dots.OriginProject && d.Slug == m.trust.lib.Slug && d.Root == m.trust.lib.Root {
					m.trust.lib = d
				}
			}
		}
		m.attached = map[string]bool{}
		if p, ok := m.snap.Pane(m.target); ok {
			for _, d := range p.Dots {
				m.attached[d.Slug] = true
			}
		}
		// Every box the developer hasn't toggled here follows the pane, so a
		// dot attached or detached elsewhere meanwhile shows as it is and
		// apply never undoes it (U13).
		for s := range m.selected {
			if !m.touched[s] && !m.attached[s] {
				delete(m.selected, s)
			}
		}
		for s := range m.attached {
			if !m.touched[s] {
				m.selected[s] = true
			}
		}
		m.cursor = max(0, min(m.cursor, len(m.library())-1))
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
			m.selected[msg.slug], m.touched[msg.slug] = true, true
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
		case scrImportSource:
			return m.keysImportSource(msg)
		case scrImportPreview:
			return m.keysImportPreview(msg)
		case scrImportRename:
			return m.keysImportRename(msg)
		case scrExport:
			return m.keysExport(msg)
		case scrTrust:
			return m.keysTrust(msg)
		}
		return m.keysLibrary(msg)
	}
	return m, nil
}

func (m Model) current() (view.LibraryDot, bool) {
	if lib := m.library(); m.cursor >= 0 && m.cursor < len(lib) {
		return lib[m.cursor], true
	}
	return view.LibraryDot{}, false
}

func (m Model) keysLibrary(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch k.String() {
	case "esc", "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.library())-1))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case " ":
		if m.target == "" {
			m.status = "focus an agent pane to attach dots"
			return m, nil
		}
		if d, ok := m.current(); ok {
			if d.Origin == dots.OriginProject && !d.Trusted && !m.selected[d.Slug] {
				m.status = style.Sym("⚠") + " " + style.Clean(d.Name) + " is an untrusted project dot: press T to review it"
				return m, nil
			}
			m.selected[d.Slug], m.touched[d.Slug] = !m.selected[d.Slug], true
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
	case "S":
		if m.d.Setup.Check == nil {
			m.status = "setup is not available here"
			return m, nil
		}
		return m, m.openSetup()
	case "i":
		if m.d.History == nil {
			m.status = "profiles are not available here"
			return m, nil
		}
		if d, ok := m.current(); ok {
			return m, m.openProfile(d)
		}
	case "I":
		m.scr, m.input = scrImportSource, newPrompt("~/security.dotpack.toml or https://...", "")
		return m, textinput.Blink
	case "T":
		if d, ok := m.current(); ok {
			if d.Origin != dots.OriginProject {
				m.status = style.Clean(d.Name) + " is your own dot; only project dots need trust"
				return m, nil
			}
			if m.target == "" {
				m.status = "focus an agent pane in that repository to trust its dots"
				return m, nil
			}
			return m, m.openTrust(d)
		}
	case "e":
		if d, ok := m.current(); ok {
			if d.Origin == dots.OriginProject {
				m.status = "project dots are edited in their repository (" + style.Clean(d.ProjectName) + "): " + projectRel(d)
				return m, nil
			}
			full, err := dots.Load(m.d.DotsDir, d.Slug)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.form, m.scr = m.newForm(full, false), scrEditor
		}
	case "D":
		if d, ok := m.current(); ok {
			if d.Origin == dots.OriginProject {
				m.status = "project dots live in their repository; press T to untrust this one"
				return m, nil
			}
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
		m.status = "no dot selected · P project memory"
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
	// The memory screen exports into the home directory (memui's default),
	// never the pane's checkout (ruling P10-R13: an export can hold global
	// and secret-confirmed memory, which `git add -A` would publish) nor
	// the popup's working directory (the plugin's install directory).
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
	for _, d := range m.library() {
		switch {
		case m.selected[d.Slug] && !m.attached[d.Slug] && d.Origin == dots.OriginProject && !d.Trusted:
			// Never attach an untrusted project dot (its file may have
			// changed since it was checked).
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
		if m.nameTaken(d) {
			m.form.SetError(fmt.Sprintf("a dot named %q already exists: pick another name", d.Name))
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

// nameTaken reports another library dot with d's display name (any case):
// two dots with one name would share one delivery header, which the loop
// guard tells dots apart by (U10). A new dot's own name clash is ErrExists.
func (m Model) nameTaken(d dots.Dot) bool {
	for _, o := range m.snap.Library {
		if (d.Slug == "" || o.Slug != d.Slug) && strings.EqualFold(strings.TrimSpace(o.Name), strings.TrimSpace(d.Name)) {
			return true
		}
	}
	return false
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

// keysConfirmMemory answers "Also delete its memory in all projects? (y/N)".
// Memory is kept by default (U22, the developer's decision): only y deletes
// it; enter, n, esc and every other key keep it.
func (m Model) keysConfirmMemory(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	slug, name, st, caller := m.delSlug, m.delName, m.d.Memory, m.d.Caller
	m.scr = scrLibrary
	if s := k.String(); s != "y" && s != "Y" {
		m.status = "deleted " + name + " (memory kept)"
		return m, nil
	}
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

// badge marks where a library dot comes from.
func badge(d view.LibraryDot) string {
	sep := " " + style.Sym("·") + " "
	switch d.Origin {
	case dots.OriginProject:
		if !d.Trusted {
			return "  " + style.Sym("⚠") + " project" + sep + "untrusted"
		}
		b := "  project"
		if len(d.Clamped) > 0 {
			b += sep + style.Clean(strings.Join(d.Clamped, ", ")) + " off"
		}
		if d.TrustedAt != "" {
			b += sep + "trusted at " + style.Clean(d.TrustedAt)
		}
		return b
	case dots.OriginTemplate:
		return "  template-derived"
	case dots.OriginImport:
		return "  imported"
	}
	return ""
}

func (m Model) View() string {
	switch m.scr {
	case scrEditor:
		h := m.height
		if m.saving && h > 1 {
			h-- // keep a line for "saving…" so the title isn't pushed off
		}
		m.form.SetHeight(h)
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
				marker = style.Sym("▸") + " "
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Sym(d.Glyph), d.Name))
		}
		b.WriteString("\n" + style.Dim.Render("j/k move · enter use · esc back"))
		return b.String()
	case scrConfirm:
		return style.Title.Render(fmt.Sprintf("Delete %s?", m.delName)) + "\n\n" + style.Dim.Render("y delete · any other key cancels")
	case scrConfirmMemory:
		return style.Title.Render(fmt.Sprintf("Deleted %s.", m.delName)) + "\nAlso delete its memory in all projects? (y/N)\n\n" +
			style.Dim.Render("y delete memory · enter or any other key keeps it (re-creating the dot picks it back up)")
	case scrMemory:
		if m.mem != nil {
			return m.mem.View()
		}
	case scrProviders:
		return m.providersView()
	case scrProfile:
		if m.prof != nil {
			v := m.prof.View()
			if m.status != "" {
				v += "\n" + m.cutLine(style.Clean(m.status))
			}
			return v
		}
	case scrImportSource, scrImportPreview, scrImportRename:
		if m.scr == scrImportSource || m.imp != nil {
			return m.importView()
		}
	case scrExport:
		if m.exp != nil {
			return m.exportView()
		}
	case scrTrust:
		if m.trust != nil {
			return m.trustView()
		}
	case scrSetup:
		if m.setup != nil {
			return m.setupView()
		}
	}
	var b strings.Builder
	head := "DOTS LIBRARY ─ target: " + m.target
	if m.target == "" {
		head = "DOTS LIBRARY ─ no agent pane focused (library only)"
	}
	b.WriteString(style.Title.Render(head) + "\n\n")
	lib := m.library()
	if len(lib) == 0 {
		b.WriteString(style.Dim.Render("No dots yet. Press t to start from a template, n for a new dot or I to import one.") + "\n")
	}
	for i, d := range lib {
		check := "[ ]"
		if m.selected[d.Slug] {
			check = "[x]"
		}
		line := m.libraryRow(fmt.Sprintf("%s %s %-14s %-22s %s", check, style.Glyph(style.Clean(d.Glyph), style.Clean(d.Color)), style.Clean(d.Name),
			style.Clean(d.Backend+"/"+d.Model), style.Clean(d.Mode)), badge(d))
		if i == m.cursor {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for _, inv := range m.snap.Invalid {
		// File names and errors can come from a repository (project dots):
		// cleaned, so no control character reaches the terminal.
		b.WriteString(style.Err.Render(fmt.Sprintf("⚠ %s:%d: %s", style.Clean(inv.File), inv.Line, style.Clean(inv.Error))) + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + strings.Join(m.fitLines([]string{style.Clean(m.status)}), "\n") + "\n")
	}
	if m.mcpStatus != "" {
		b.WriteString("\n" + style.Dim.Render(m.mcpStatus) + "\n")
	}
	if m.onboardNote != "" {
		b.WriteString(strings.Join(m.fitLines([]string{style.Err.Render(m.onboardNote)}), "\n") + "\n")
	}
	help := "space toggle · enter apply · n new · t template · e edit · D delete · i profile · I import · T trust · K providers · esc close"
	if m.d.Memory != nil {
		help += " · M memory · P project memory"
	}
	if m.d.Setup.Check != nil {
		help += " · S setup"
	}
	help = strings.ReplaceAll(help, " · ", " "+style.Sym("·")+" ")
	b.WriteString("\n" + style.Dim.Render(strings.Join(m.fitLines([]string{help}), "\n")))
	return b.String()
}
