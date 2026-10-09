// Package memui is the memory browser and editor embedded in the manage popup:
// list, filter, approve, pin, edit, add and forget memory entries.
package memui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
)

type Deps struct {
	Store   *memory.Store
	Project memory.Project // zero ID: no project (only a dot's global memory)
	Dot     string         // dot slug; "" opens project memory
	DotName string
	Notify  func() // tells the engine memory changed; may be nil
	Notice  string // shown as the first status line (e.g. why project memory is missing)
}

// errChanged stops approve/pin when the stored text is no longer the text on screen.
var errChanged = errors.New("this entry changed since it was listed; review it again")

// BackMsg asks the parent to close the memory screen.
type BackMsg struct{}

type (
	loadedMsg struct {
		gen     int // the view generation that asked; stale answers are dropped
		entries []memory.Entry
		err     error
	}
	changedMsg struct {
		status string
		err    error
	}
)

type mode int

const (
	modeBrowse mode = iota
	modeConfirmForget
	modeConfirmForgetAll
	modeConfirmApprove
	modeEdit
	modeFilter
)

type scopeView int

const (
	viewDot scopeView = iota
	viewProject
	viewGlobal
	viewAll
)

func (v scopeView) String() string {
	return [...]string{"dot", "project", "global", "all"}[v]
}

type section struct {
	title string
	match func(memory.Entry) bool
}

func isKind(e memory.Entry, ks ...memory.Kind) bool { return slices.Contains(ks, e.Kind) }

var sections = []section{
	{"PROPOSALS", func(e memory.Entry) bool { return e.Status == memory.StatusProposed }},
	{"CONVENTIONS & DECISIONS", func(e memory.Entry) bool {
		return e.Status != memory.StatusProposed && isKind(e, memory.KindConvention, memory.KindDecision, memory.KindPreference)
	}},
	{"FINDINGS", func(e memory.Entry) bool { return e.Status != memory.StatusProposed && isKind(e, memory.KindFinding) }},
	{"DISMISSED", func(e memory.Entry) bool { return e.Status != memory.StatusProposed && isKind(e, memory.KindDismissed) }},
	{"NOTES", func(e memory.Entry) bool {
		return e.Status != memory.StatusProposed && !isKind(e, memory.KindConvention, memory.KindDecision,
			memory.KindPreference, memory.KindFinding, memory.KindDismissed)
	}},
}

func sectionOf(e memory.Entry) int {
	for i, s := range sections {
		if s.match(e) {
			return i
		}
	}
	return len(sections) - 1
}

type Model struct {
	d        Deps
	views    []scopeView
	vi       int
	entries  []memory.Entry
	rows     []memory.Entry // filtered, in section order
	cursor   int
	gen      int        // bumped when the viewed scope changes
	forget   memory.Ref // the entry the forget prompt is about
	forgetP  string
	approve  memory.Entry // the proposal the approve screen shows
	mode     mode
	filter   string
	input    textinput.Model    // filter text, forget-all confirmation
	fields   [3]textinput.Model // editor: text, kind, paths
	field    int
	editRef  *memory.Ref // nil: a new entry
	editPid  string
	secretOK bool // the developer confirmed a secret warning
	loadErr  string
	status   string
	width    int
	height   int
}

// clean makes stored text safe for one terminal line: no control characters
// (ledger bodies and project names are not sanitized at storage) and
// whitespace collapsed.
func clean(s string) string {
	return strings.Join(strings.Fields(textutil.SanitizeModelText(s)), " ")
}

func New(d Deps) Model {
	var views []scopeView
	switch {
	case d.Dot != "" && d.Project.ID != "":
		views = []scopeView{viewDot, viewProject, viewGlobal, viewAll}
	case d.Dot != "":
		views = []scopeView{viewGlobal}
	case d.Project.ID != "":
		views = []scopeView{viewProject, viewAll}
	}
	m := Model{d: d, views: views, input: textinput.New(), status: d.Notice}
	for i, ph := range []string{"one sentence", "convention | decision | finding | dismissed | preference | note", "globs, comma-separated (optional)"} {
		m.fields[i] = textinput.New()
		m.fields[i].Placeholder = ph
		m.fields[i].CharLimit = 600
	}
	return m
}

func (m Model) Init() tea.Cmd { return m.load() }

func (m Model) view() scopeView {
	if len(m.views) == 0 {
		return viewProject
	}
	return m.views[m.vi]
}

func (m Model) load() tea.Cmd {
	if len(m.views) == 0 {
		return func() tea.Msg { return loadedMsg{gen: m.gen, err: errors.New("no project: focus an agent pane first")} }
	}
	st, pid, dot, v, gen := m.d.Store, m.d.Project.ID, m.d.Dot, m.view(), m.gen
	return func() tea.Msg {
		f := memory.Filter{Dot: dot}
		switch v {
		case viewDot:
			f.Scope = memory.ScopeDot
		case viewProject:
			f = memory.Filter{Scope: memory.ScopeProject}
		case viewGlobal:
			f.Scope, pid = memory.ScopeGlobal, ""
		}
		es, err := st.List(pid, f)
		return loadedMsg{gen: gen, entries: es, err: err}
	}
}

// rebuild re-filters the entries and keeps the cursor on the same entry (by
// ref), so a reload never moves the selection onto a different one.
func (m *Model) rebuild() {
	prev, had := m.selected()
	m.rows = nil
	q := strings.ToLower(m.filter)
	for si := range sections {
		for _, e := range m.entries {
			if sectionOf(e) != si {
				continue
			}
			hay := strings.ToLower(e.Text + " " + strings.Join(e.Paths, " ") + " " + string(e.Kind))
			if q == "" || strings.Contains(hay, q) {
				m.rows = append(m.rows, e)
			}
		}
	}
	m.cursor = max(0, min(m.cursor, len(m.rows)-1))
	if had {
		for i, e := range m.rows {
			if e.Ref() == prev.Ref() {
				m.cursor = i
				break
			}
		}
	}
}

func (m Model) selected() (memory.Entry, bool) {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor], true
	}
	return memory.Entry{}, false
}

func (m Model) pidOf(e memory.Entry) string {
	if e.Scope == memory.ScopeGlobal {
		return ""
	}
	return m.d.Project.ID
}

// mutate runs a store change off the UI goroutine, then notifies the engine.
func (m Model) mutate(status string, fn func(st *memory.Store) error) tea.Cmd {
	st, notify := m.d.Store, m.d.Notify
	return func() tea.Msg {
		if err := fn(st); err != nil {
			return changedMsg{err: err}
		}
		if notify != nil {
			notify()
		}
		return changedMsg{status: status}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.entries, m.loadErr = msg.entries, ""
		if msg.err != nil {
			m.loadErr = msg.err.Error()
		}
		m.rebuild()
	case changedMsg:
		if msg.err != nil {
			m.status = clean(msg.err.Error())
			return m, m.load()
		}
		m.status = msg.status
		return m, m.load()
	case tea.KeyMsg:
		switch m.mode {
		case modeConfirmForget:
			return m.keysConfirmForget(msg)
		case modeConfirmForgetAll:
			return m.keysConfirmForgetAll(msg)
		case modeConfirmApprove:
			return m.keysConfirmApprove(msg)
		case modeEdit:
			return m.keysEdit(msg)
		case modeFilter:
			return m.keysFilter(msg)
		}
		return m.keysBrowse(msg)
	}
	return m, nil
}

func (m Model) keysBrowse(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch k.String() {
	case "esc", "q":
		return m, func() tea.Msg { return BackMsg{} }
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.rows)-1))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "tab":
		if len(m.views) > 1 {
			m.vi, m.cursor, m.gen = (m.vi+1)%len(m.views), 0, m.gen+1
			m.entries, m.rows, m.loadErr = nil, nil, ""
			return m, m.load()
		}
	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.CursorEnd()
		m.input.Focus()
		return m, textinput.Blink
	case "a":
		e, ok := m.selected()
		if !ok {
			return m, nil
		}
		if e.Status != memory.StatusProposed {
			m.status = "only proposals need approval"
			return m, nil
		}
		// Rows are cut to the screen width: show the whole proposal first.
		m.mode, m.approve = modeConfirmApprove, e
		return m, nil
	case "p":
		e, ok := m.selected()
		if !ok {
			return m, nil
		}
		ref, pid, pin, seen := e.Ref(), m.pidOf(e), !e.Pinned, e.Text
		label := "pinned "
		if !pin {
			label = "unpinned "
		}
		return m, m.mutate(label+ref.String(), func(st *memory.Store) error {
			_, err := st.Update(pid, ref, func(x *memory.Entry) error {
				if x.Text != seen {
					return errChanged
				}
				x.Pinned = pin
				return nil
			})
			return err
		})
	case "x":
		if e, ok := m.selected(); ok {
			m.mode, m.forget, m.forgetP = modeConfirmForget, e.Ref(), m.pidOf(e)
		}
	case "X":
		if len(m.views) == 0 {
			return m, nil
		}
		if m.view() == viewAll {
			m.status = "press tab to pick one scope before forgetting all"
			return m, nil
		}
		m.mode = modeConfirmForgetAll
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "e":
		if e, ok := m.selected(); ok {
			return m.startEdit(&e)
		}
	case "n":
		if len(m.views) == 0 {
			return m, nil
		}
		return m.startEdit(nil)
	}
	return m, nil
}

func (m Model) keysConfirmForget(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeBrowse
	if k.String() != "y" {
		m.status = "kept"
		return m, nil
	}
	ref, pid := m.forget, m.forgetP
	return m, m.mutate("forgot "+ref.String(), func(st *memory.Store) error { return st.Forget(pid, ref, false) })
}

// keysConfirmApprove approves the proposal on the confirm screen on y; any
// other key cancels. The text shown must still be the stored text.
func (m Model) keysConfirmApprove(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeBrowse
	if k.String() != "y" {
		m.status = "not approved"
		return m, nil
	}
	e := m.approve
	ref, pid, seen := e.Ref(), m.pidOf(e), e.Text
	return m, m.mutate("approved "+ref.String(), func(st *memory.Store) error {
		_, err := st.Update(pid, ref, func(x *memory.Entry) error {
			if x.Text != seen {
				return errChanged
			}
			x.Status = memory.StatusActive
			return nil
		})
		return err
	})
}

// wrap breaks clean(s) into lines of at most w runes, on spaces where it can.
func wrap(s string, w int) []string {
	w = max(w, 10)
	var lines []string
	var cur []rune
	for _, word := range strings.Fields(clean(s)) {
		r := []rune(word)
		for len(r) > w { // a word longer than a line is cut
			if len(cur) > 0 {
				lines, cur = append(lines, string(cur)), nil
			}
			lines, r = append(lines, string(r[:w])), r[w:]
		}
		switch {
		case len(cur) == 0:
			cur = r
		case len(cur)+1+len(r) <= w:
			cur = append(append(cur, ' '), r...)
		default:
			lines, cur = append(lines, string(cur)), r
		}
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	return lines
}

// approveScreen is the full proposal: who proposed it, its kind, its paths
// and every word of its text.
func (m Model) approveScreen(w int) []string {
	e := m.approve
	by := clean(e.Dot)
	if by == "" {
		by = "the developer"
	}
	paths := clean(strings.Join(e.Paths, ", "))
	if paths == "" {
		paths = "(none)"
	}
	lines := []string{style.Title.Render("APPROVE " + clean(e.Ref().String()) + "?"),
		"proposed by: " + by, "kind:        " + clean(string(e.Kind))}
	for i, l := range wrap(paths, w-13) {
		if i == 0 {
			lines = append(lines, "paths:       "+l)
		} else {
			lines = append(lines, "             "+l)
		}
	}
	lines = append(lines, "text:")
	for _, l := range wrap(e.Text, w-2) {
		lines = append(lines, "  "+l)
	}
	return lines
}

// forgetAllTarget is the scope "X" clears and the name the developer must type.
func (m Model) forgetAllTarget() (memory.Scope, string, string, string) {
	switch m.view() {
	case viewDot:
		return memory.ScopeDot, m.d.Dot, m.d.Project.ID, clean(m.d.Dot)
	case viewGlobal:
		return memory.ScopeGlobal, m.d.Dot, "", clean(m.d.Dot)
	}
	// A name that cleans to nothing would make an empty answer confirm.
	name := clean(m.d.Project.Name)
	if name == "" {
		name = clean(m.d.Project.ID)
	}
	return memory.ScopeProject, "", m.d.Project.ID, name
}

func (m Model) keysConfirmForgetAll(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeBrowse
		return m, nil
	case "enter":
		m.mode = modeBrowse
		sc, dot, pid, want := m.forgetAllTarget()
		if strings.TrimSpace(m.input.Value()) != want {
			m.status = fmt.Sprintf("nothing forgotten: type %q to confirm", want)
			return m, nil
		}
		return m, m.mutate("forgot all "+string(sc)+" memory", func(st *memory.Store) error {
			_, err := st.ForgetAll(pid, sc, dot)
			return err
		})
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// newTarget is where "n" adds an entry: the scope being viewed.
func (m Model) newTarget() (memory.Scope, string, string) {
	switch m.view() {
	case viewProject:
		return memory.ScopeProject, "", m.d.Project.ID
	case viewGlobal:
		return memory.ScopeGlobal, m.d.Dot, ""
	case viewAll:
		if m.d.Dot == "" {
			return memory.ScopeProject, "", m.d.Project.ID
		}
	}
	return memory.ScopeDot, m.d.Dot, m.d.Project.ID
}

func defaultKind(sc memory.Scope) memory.Kind {
	switch sc {
	case memory.ScopeProject:
		return memory.KindConvention
	case memory.ScopeGlobal:
		return memory.KindPreference
	}
	return memory.KindNote
}

func (m Model) startEdit(e *memory.Entry) (tea.Model, tea.Cmd) {
	m.mode, m.field, m.secretOK = modeEdit, 0, false
	vals := [3]string{}
	if e != nil {
		r := e.Ref()
		m.editRef, m.editPid = &r, m.pidOf(*e)
		vals = [3]string{e.Text, string(e.Kind), strings.Join(e.Paths, ", ")}
	} else {
		sc, _, _ := m.newTarget()
		m.editRef, m.editPid = nil, ""
		vals[1] = string(defaultKind(sc))
	}
	for i := range m.fields {
		m.fields[i].SetValue(vals[i])
		m.fields[i].CursorEnd()
		m.fields[i].Blur()
	}
	m.fields[0].Focus()
	return m, textinput.Blink
}

func splitPaths(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m Model) keysEdit(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeBrowse
		return m, nil
	case "tab", "shift+tab":
		step := 1
		if k.String() == "shift+tab" {
			step = len(m.fields) - 1
		}
		m.fields[m.field].Blur()
		m.field = (m.field + step) % len(m.fields)
		m.fields[m.field].Focus()
		return m, textinput.Blink
	case "ctrl+s":
		text := strings.TrimSpace(m.fields[0].Value())
		kind := memory.Kind(strings.TrimSpace(m.fields[1].Value()))
		paths := splitPaths(m.fields[2].Value())
		if !kind.Valid() {
			m.status = fmt.Sprintf("kind must be one of %v", memory.Kinds)
			return m, nil
		}
		if found, what := memory.ScanSecrets(text + " " + strings.Join(paths, " ")); found && !m.secretOK {
			m.secretOK = true
			m.status = fmt.Sprintf("this looks like a secret (%s): ctrl+s again to save anyway, esc to cancel", what)
			return m, nil
		}
		m.mode = modeBrowse
		if m.editRef != nil {
			ref, pid := *m.editRef, m.editPid
			return m, m.mutate("saved "+ref.String(), func(st *memory.Store) error {
				_, err := st.Update(pid, ref, func(x *memory.Entry) error {
					x.Text, x.Kind, x.Paths, x.Source = text, kind, paths, memory.SourceUser
					return nil
				})
				return err
			})
		}
		sc, dot, pid := m.newTarget()
		e := memory.Entry{Scope: sc, Dot: dot, Kind: kind, Text: text, Paths: paths, Source: memory.SourceUser, Status: memory.StatusActive}
		return m, m.mutate("added", func(st *memory.Store) error {
			_, err := st.Add(pid, e)
			return err
		})
	}
	m.secretOK = false
	var cmd tea.Cmd
	m.fields[m.field], cmd = m.fields[m.field].Update(k)
	return m, cmd
}

func (m Model) keysFilter(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		m.filter, m.mode = strings.TrimSpace(m.input.Value()), modeBrowse
		m.rebuild()
		return m, nil
	case "esc":
		m.filter, m.mode = "", modeBrowse
		m.rebuild()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func trunc(s string, n int) string {
	r := []rune(clean(s))
	if len(r) > n {
		return string(r[:max(1, n-1)]) + "…"
	}
	return string(r)
}

func (m Model) row(e memory.Entry) string {
	star := " "
	if e.Pinned {
		star = "★"
	}
	var who string
	switch {
	case e.Source == memory.SourceUser:
		who = "(you)"
	case e.Status == memory.StatusProposed || (e.Scope == memory.ScopeProject && e.Dot != ""):
		who = "(" + clean(e.Dot) + ")"
	default:
		who = fmt.Sprintf("used %d×", e.Uses)
	}
	return fmt.Sprintf("%-7s %s %-10s %-9s %s", clean(e.Ref().String()), star, clean(string(e.Kind)), who, clean(e.Text))
}

// list renders the rows with their section headers and reports the line the
// cursor is on.
func (m Model) list(w int) (lines []string, cur int) {
	counts := make([]int, len(sections))
	for _, e := range m.entries {
		counts[sectionOf(e)]++
	}
	last := -1
	for i, e := range m.rows {
		if s := sectionOf(e); s != last {
			title := sections[s].title
			if s == 0 {
				title = fmt.Sprintf("%s (%d)", title, counts[s])
			}
			lines = append(lines, style.Dim.Render("── "+title+" "+strings.Repeat("─", max(0, w-len(title)-4))))
			last = s
		}
		line := trunc(m.row(e), max(20, w-2))
		if i == m.cursor {
			cur = len(lines)
			lines = append(lines, style.Selected.Render("▸ "+line))
		} else {
			lines = append(lines, "  "+line)
		}
	}
	return lines, cur
}

func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var head strings.Builder
	who := clean(m.d.DotName)
	if who == "" {
		who = clean(m.d.Dot)
	}
	if who == "" {
		who = "project"
	}
	proj := "no project"
	if m.d.Project.ID != "" {
		proj = fmt.Sprintf("project %s (%s)", clean(m.d.Project.Name), clean(m.d.Project.ID))
	}
	head.WriteString(style.Title.Render(fmt.Sprintf("MEMORY → %s · %s ── %d entries", who, proj, len(m.entries))) + "\n")
	filter := ""
	if m.filter != "" {
		filter = "/" + clean(m.filter)
	}
	head.WriteString(fmt.Sprintf("Filter: %-30s scope: %s\n", filter, m.view()))
	if m.loadErr != "" {
		head.WriteString(style.Err.Render(clean(m.loadErr)) + "\n")
	}

	var tail strings.Builder
	switch m.mode {
	case modeConfirmForget:
		tail.WriteString("\n" + style.Title.Render("Forget "+clean(m.forget.String())+"?") + " y forget · any other key keeps it\n")
	case modeConfirmForgetAll:
		sc, _, _, want := m.forgetAllTarget()
		tail.WriteString(fmt.Sprintf("\n%s Type %q and press enter:\n%s\n", style.Title.Render("Forget ALL "+string(sc)+" memory?"), want, m.input.View()))
	case modeConfirmApprove:
		tail.WriteString("\ny approve · any other key cancels\n")
	case modeFilter:
		tail.WriteString("\n/" + m.input.View() + "\n")
	case modeEdit:
		title := "NEW ENTRY"
		if m.editRef != nil {
			title = "EDIT " + clean(m.editRef.String())
		}
		tail.WriteString("\n" + style.Title.Render(title) + "\n")
		for i, label := range []string{"text ", "kind ", "paths"} {
			marker := "  "
			if i == m.field {
				marker = "▸ "
			}
			tail.WriteString(marker + label + " " + m.fields[i].View() + "\n")
		}
		tail.WriteString(style.Dim.Render("ctrl+s save · tab next field · esc cancel") + "\n")
	}
	footer := "a approve  e edit  p pin  x forget  X forget all  n new  / filter  tab scope  esc back"
	if m.status != "" {
		footer = clean(m.status)
	}
	tail.WriteString(style.Dim.Render(footer))

	lines, cur := m.list(w)
	if m.mode == modeConfirmApprove {
		lines, cur = m.approveScreen(w), 0
	} else if len(m.rows) == 0 && m.loadErr == "" {
		lines = []string{style.Dim.Render("No memory here yet. Press n to add an entry.")}
	}
	// With a known height, scroll the list so the selected row stays visible.
	// The approve screen is never cut: every word of a proposal must show.
	if m.height > 0 && m.mode != modeConfirmApprove {
		used := strings.Count(head.String(), "\n") + strings.Count(tail.String(), "\n") + 1
		if avail := max(3, m.height-used); len(lines) > avail {
			start := max(0, min(cur-avail/2, len(lines)-avail))
			lines = lines[start : start+avail]
		}
	}
	return head.String() + strings.Join(lines, "\n") + "\n" + tail.String()
}
