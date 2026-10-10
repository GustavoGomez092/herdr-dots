// Package memui is the memory browser and editor embedded in the manage popup:
// list, filter, approve, pin, edit, add and forget memory entries, and
// export, import and purge them.
package memui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/platform"
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
	// Home is where E exports and relative E/I paths resolve; "" is the
	// developer's home directory (tests set it). Never the popup's working
	// directory (the plugin's install directory) or a pane's checkout:
	// ruling P10-R13, an export can hold global and secret-confirmed memory.
	Home string
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
	importReadMsg struct { // a file I read and parsed, for the preview
		path    string
		entries []memory.Entry
		hits    []int // the numbers of the entries that look like secrets
		err     error
	}
	purgeCountMsg struct { // how many entries P would delete; -1: unknown
		n int
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
	modeExportPath
	modeImportPath
	modeImportPreview
	modeConfirmPurge
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
	editSeen string          // the text the editor loaded
	secretOK bool            // the developer confirmed a secret warning
	path     textinput.Model // export and import paths, the purge answer
	gitDir   string          // the folder the export path's git check last looked at
	inGit    bool            // gitDir is inside a git work tree: the export screen warns
	imp      []memory.Entry  // the parsed file the import preview shows
	impPath  string
	impHits  []int // the numbers of the previewed entries that look like secrets
	purgeN   int   // entries the purge screen says will go; -1: unknown
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
	if d.Home == "" {
		d.Home, _ = os.UserHomeDir()
	}
	m := Model{d: d, views: views, input: textinput.New(), path: textinput.New(), status: d.Notice}
	m.path.Width = m.pathWidth()
	for i, ph := range []string{"one sentence", "convention | decision | finding | dismissed | preference | note | research | question", "globs, comma-separated (optional)"} {
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
	st, gen := m.d.Store, m.gen
	pid, f := m.viewFilter()
	return func() tea.Msg {
		es, err := st.List(pid, f)
		return loadedMsg{gen: gen, entries: es, err: err}
	}
}

// viewFilter is the project id and filter of the scope being viewed. The
// all-scopes view lists this project's memory and the global memory.
func (m Model) viewFilter() (string, memory.Filter) {
	pid, f := m.d.Project.ID, memory.Filter{Dot: m.d.Dot}
	switch m.view() {
	case viewDot:
		f.Scope = memory.ScopeDot
	case viewProject:
		f = memory.Filter{Scope: memory.ScopeProject}
	case viewGlobal:
		f.Scope, pid = memory.ScopeGlobal, ""
	}
	return pid, f
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
	return m.mutateSaying(func(st *memory.Store) (string, error) { return status, fn(st) })
}

// mutateSaying is mutate with a status line that fn picks from what it did.
func (m Model) mutateSaying(fn func(st *memory.Store) (string, error)) tea.Cmd {
	st, notify := m.d.Store, m.d.Notify
	return func() tea.Msg {
		status, err := fn(st)
		if err != nil {
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
		m.path.Width = m.pathWidth()
		m.path.SetCursor(m.path.Position()) // re-scroll to the new width
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
	case importReadMsg:
		return m.importRead(msg)
	case purgeCountMsg:
		if m.mode != modeBrowse || m.d.Project.ID == "" {
			return m, nil // the developer moved on
		}
		m.purgeN = msg.n
		return m.startPath(modeConfirmPurge, "")
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
		case modeExportPath, modeImportPath:
			return m.keysPath(msg)
		case modeImportPreview:
			return m.keysImportPreview(msg)
		case modeConfirmPurge:
			return m.keysConfirmPurge(msg)
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
		return m, m.mutateSaying(func(st *memory.Store) (string, error) {
			was := false
			got, err := st.Update(pid, ref, func(x *memory.Entry) error {
				if x.Text != seen {
					return errChanged
				}
				was = x.Status == memory.StatusArchived
				x.Pinned = pin
				return nil
			})
			return archivedSay(label+ref.String(), got, was), err
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
	case "E":
		if len(m.views) == 0 {
			return m, nil
		}
		return m.startPath(modeExportPath, filepath.Join(m.d.Home, m.exportName(time.Now())))
	case "I":
		if len(m.views) == 0 {
			return m, nil
		}
		return m.startPath(modeImportPath, "")
	case "P":
		if m.d.Project.ID == "" {
			m.status = "no project on this screen"
			return m, nil
		}
		return m, m.countPurge()
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
	if !m.approveFits() {
		m.status = "not approved: the popup is too small to show the whole proposal"
		return m, nil
	}
	e := m.approve
	ref, pid, seen := e.Ref(), m.pidOf(e), e.Text
	return m, m.mutateSaying(func(st *memory.Store) (string, error) {
		was := false
		got, err := st.Update(pid, ref, func(x *memory.Entry) error {
			if x.Text != seen {
				return errChanged
			}
			was = x.Status == memory.StatusArchived
			x.Status = memory.StatusActive
			return nil
		})
		return archivedSay("approved "+ref.String(), got, was), err
	})
}

// archivedSay adds to a success line when the cap archived the entry at once
// (an import approved or unpinned in a full scope, P9-R15).
func archivedSay(status string, got memory.Entry, was bool) string {
	if got.Status != memory.StatusArchived || was {
		return status
	}
	whose := "this dot's"
	switch got.Scope {
	case memory.ScopeProject:
		whose = "this project's"
	case memory.ScopeGlobal:
		whose = "global"
	}
	if got.Source != memory.SourceImport {
		return status + ", then archived: " + whose + " memory is full and research notes give way first"
	}
	return status + ", then archived: " + whose + " memory is full and imports give way to your own entries"
}

// wrap breaks clean(s) into lines of at most w display cells (a CJK rune
// takes two), on spaces where it can.
func wrap(s string, w int) []string {
	w = max(w, 10)
	var lines []string
	cur, curW := "", 0
	for _, word := range strings.Fields(clean(s)) {
		ww := ansi.StringWidth(word)
		for ww > w { // a word longer than a line is cut, at a rune boundary
			if cur != "" {
				lines, cur, curW = append(lines, cur), "", 0
			}
			head, rest := cutCells(word, w)
			// Per-rune widths can differ from a grapheme cluster's (an emoji
			// with VS16): a head still too wide is kept whole, and
			// approveFits, which measures lines, refuses it.
			lines, word = append(lines, head), rest
			ww = ansi.StringWidth(word)
		}
		switch {
		case word == "": // cut to the end
		case cur == "":
			cur, curW = word, ww
		case curW+1+ww <= w:
			cur, curW = cur+" "+word, curW+1+ww
		default:
			lines, cur, curW = append(lines, cur), word, ww
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// cutCells splits s after at most w display cells, at a rune boundary; the
// head is never empty, so a caller cutting in a loop always makes progress.
func cutCells(s string, w int) (head, rest string) {
	used := 0
	for i, r := range s {
		rw := ansi.StringWidth(string(r))
		if used+rw > w && i > 0 {
			return s[:i], s[i:]
		}
		used += rw
	}
	return s, ""
}

// pathField is a labelled path cut into lines of at most w display cells
// with every character kept (clean would collapse its spaces): control
// characters are dropped, and a newline or tab shows as \n or \t.
func pathField(label, p string, w int) []string {
	pad := strings.Repeat(" ", ansi.StringWidth(label))
	s := strings.NewReplacer("\n", `\n`, "\t", `\t`).Replace(textutil.SanitizeModelText(p))
	var lines []string
	for prefix := label; ; prefix = pad {
		var head string
		head, s = cutCells(s, max(1, w-len(pad)))
		lines = append(lines, prefix+head)
		if s == "" {
			return lines
		}
	}
}

// field is a labelled approve-screen value wrapped to w cells, its
// continuation lines under the value.
func field(label, value string, w int) []string {
	pad := strings.Repeat(" ", ansi.StringWidth(label))
	var lines []string
	for i, l := range wrap(value, w-len(pad)) {
		if i == 0 {
			lines = append(lines, label+l)
		} else {
			lines = append(lines, pad+l)
		}
	}
	if len(lines) == 0 {
		lines = []string{label}
	}
	return lines
}

// approveTail is the lines View adds under the approve screen (and every
// other whole-popup screen): a blank line, the prompt and the footer.
const approveTail = 3

// screenWidth is the width View lays out for.
func (m Model) screenWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// approveMinWidth is the narrowest popup the approve screen (and every other
// whole-popup screen) is laid out in.
const approveMinWidth = 24

// approveFits reports whether the whole approve screen fits the popup. A
// taller view would have its top lines (who proposed it, the kind, the
// paths) cut by the terminal, and a line wider than the popup loses its
// right end, so approving is refused until every cell fits. An unknown
// height (no WindowSizeMsg yet) fits.
func (m Model) approveFits() bool { return m.fits(m.approveScreen(m.screenWidth())) }

// fits reports whether a whole-popup screen of lines, with approveTail lines
// under it, is on screen to the last cell: what the developer confirms must
// be shown whole (Task 10).
func (m Model) fits(lines []string) bool {
	w := m.screenWidth()
	if w < approveMinWidth {
		return false
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > w {
			return false
		}
	}
	return m.height <= 0 || len(lines)+approveTail <= m.height
}

// tooSmall is what View shows instead of a screen that doesn't fit: its title
// and why, wrapped by display cells.
func (m Model) tooSmall(title string, lines []string, what, action string) []string {
	w := m.screenWidth()
	why := fmt.Sprintf("needs %d lines, has %d", len(lines)+approveTail, m.height)
	if w < approveMinWidth {
		why = fmt.Sprintf("needs %d columns, has %d", approveMinWidth, w)
	}
	return append([]string{style.Title.Render(title)},
		wrap("Too small to show the whole "+what+" ("+why+"). Enlarge the popup to "+action+".", w)...)
}

// approveScreen is the full proposal: who proposed it, its kind, its paths
// and every word of its text.
func (m Model) approveScreen(w int) []string {
	e := m.approve
	by := clean(e.Dot)
	switch {
	case e.Source == memory.SourceAgent:
		by = "the main agent"
	case by == "":
		by = "the developer"
	}
	switch {
	case e.Source == memory.SourceImport && e.From != "":
		by += " (imported from a dot pack)"
	case e.Source == memory.SourceImport:
		by += " (restored from backup)" // Phase 2's `memory import` of an export
	}
	paths := clean(strings.Join(e.Paths, ", "))
	if paths == "" {
		paths = "(none)"
	}
	var lines []string
	for _, l := range wrap("APPROVE "+clean(e.Ref().String())+"?", w) {
		lines = append(lines, style.Title.Render(l))
	}
	lines = append(lines, field("proposed by: ", by, w)...)
	lines = append(lines, field("kind:        ", clean(string(e.Kind)), w)...)
	lines = append(lines, field("paths:       ", paths, w)...)
	lines = append(lines, "text:")
	for _, l := range wrap(e.Text, w-2) {
		lines = append(lines, "  "+l)
	}
	return lines
}

// whole is a screen that takes the whole popup: what View shows, the prompt
// under it, and what replaces both when it doesn't fit.
type whole struct {
	lines          []string
	title          string // the title of the "Too small" screen
	what, action   string // "Too small to show the whole <what>… to <action>."
	prompt, cancel string // the prompt when it fits, and when it doesn't
}

// wholeScreen is the whole-popup screen of the current mode, if it has one.
func (m Model) wholeScreen(w int) (whole, bool) {
	switch m.mode {
	case modeConfirmApprove:
		return whole{m.approveScreen(w), "APPROVE " + clean(m.approve.Ref().String()) + "?", "proposal", "approve",
			"y approve · any other key cancels", "any key cancels"}, true
	case modeExportPath:
		return whole{m.pathScreen(w), "EXPORT MEMORY", "export screen", "export",
			"enter writes · esc cancels", "esc cancels"}, true
	case modeImportPath:
		return whole{m.pathScreen(w), "IMPORT MEMORY", "import screen", "import",
			"enter reads the file · esc cancels", "esc cancels"}, true
	case modeImportPreview:
		yes := "y"
		if len(m.impHits) > 0 {
			yes = "Y"
		}
		return whole{m.previewScreen(w), "IMPORT MEMORY?", "preview", "import",
			yes + " imports · any other key cancels", "any key cancels"}, true
	case modeConfirmPurge:
		return whole{m.purgeScreen(w), "PURGE PROJECT MEMORY?", "confirmation", "purge",
			"enter purges · esc keeps it", "esc keeps it"}, true
	}
	return whole{}, false
}

// shownWhole reports whether the current whole-popup screen is on screen to
// the last cell; its confirm key does nothing otherwise.
func (m Model) shownWhole() bool {
	s, ok := m.wholeScreen(m.screenWidth())
	return ok && m.fits(s.lines)
}

var allStatuses = []memory.Status{memory.StatusActive, memory.StatusProposed, memory.StatusArchived}

// maxImportBytes caps the file I reads (a memory export is small).
const maxImportBytes = 4 << 20

// pathWidth is the path input's width: the prompt, the text and the cursor
// fit the popup, and a longer path scrolls (the screen shows it whole too).
func (m Model) pathWidth() int { return max(1, m.screenWidth()-4) }

// startPath opens the path input (or the purge answer) of mode md.
func (m Model) startPath(md mode, value string) (tea.Model, tea.Cmd) {
	m.mode = md
	m.path.Width = m.pathWidth()
	m.path.SetValue(value)
	m.path.CursorEnd()
	m.path.Focus()
	m.gitDir, m.inGit = "", false
	return m.checkGit(), textinput.Blink
}

// gitWarning is the export screen's line for a target inside a git work tree.
const gitWarning = "Warning: this folder is in a git repository; the export may be committed."

// checkGit re-checks, on the export screen, whether the folder the path
// resolves to is inside a git work tree. It looks again only when that
// folder changes (a few Lstat calls, not on every key of a file name).
func (m Model) checkGit() Model {
	if m.mode != modeExportPath {
		return m
	}
	p := m.resolvePath(m.path.Value())
	if p == "" {
		m.gitDir, m.inGit = "", false
		return m
	}
	if dir := filepath.Dir(p); dir != m.gitDir {
		m.gitDir, m.inGit = dir, inGitWorkTree(dir)
	}
	return m
}

// inGitWorkTree reports whether dir, or a folder above it, holds a .git
// entry (a directory, or the file of a worktree or submodule checkout).
// dir need not exist yet.
func inGitWorkTree(dir string) bool {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// exportName is E's file name: herdr-dots-memory-<project name or
// "global">-<YYYYMMDD>.toml, with the name made safe for a file name.
func (m Model) exportName(now time.Time) string {
	name := "global"
	if m.d.Project.ID != "" {
		name = fileSafe(m.d.Project.Name)
		if name == "" {
			name = fileSafe(m.d.Project.ID)
		}
	}
	return "herdr-dots-memory-" + name + "-" + now.Format("20060102") + ".toml"
}

// fileSafe keeps ASCII letters, digits, '.', '_' and '-' and turns every
// other run of characters into one '-'.
func fileSafe(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		switch {
		case ok:
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-.")
}

// resolvePath expands a leading ~ to the home directory and resolves a
// relative path against the screen's working directory.
func (m Model) resolvePath(in string) string {
	p := strings.TrimSpace(in)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if p != "" && !filepath.IsAbs(p) && m.d.Home != "" {
		p = filepath.Join(m.d.Home, p)
	}
	return p
}

// pathScreen is the export or import path input, with the path it resolves
// to shown whole.
func (m Model) pathScreen(w int) []string {
	title, about := "EXPORT "+strings.ToUpper(m.view().String())+" MEMORY", "Writes every entry of this view, archived ones too, to a new file only you can read. An existing file is never overwritten."
	if m.mode == modeImportPath {
		title, about = "IMPORT MEMORY", "Reads a memory export (TOML). Nothing is stored before you confirm the preview."
	}
	var lines []string
	for _, l := range wrap(title, w) {
		lines = append(lines, style.Title.Render(l))
	}
	lines = append(lines, wrap(about, w)...)
	lines = append(lines, m.path.View())
	if p := m.resolvePath(m.path.Value()); p != "" {
		lines = append(lines, pathField("file: ", p, w)...)
	}
	if m.mode == modeExportPath && m.inGit {
		for _, l := range wrap(gitWarning, w) {
			lines = append(lines, style.Err.Render(l))
		}
	}
	return lines
}

func (m Model) keysPath(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	verb := "exported"
	if m.mode == modeImportPath {
		verb = "imported"
	}
	switch k.String() {
	case "esc":
		m.mode, m.status = modeBrowse, "nothing "+verb
		return m, nil
	case "enter":
		fits, md, p := m.shownWhole(), m.mode, m.resolvePath(m.path.Value())
		m.mode = modeBrowse
		switch {
		case !fits:
			m.status = "nothing " + verb + ": the popup is too small to show the whole path"
			return m, nil
		case p == "":
			m.status = "nothing " + verb + ": no file given"
			return m, nil
		case md == modeExportPath:
			return m, m.export(p)
		}
		return m, readImport(p)
	}
	var cmd tea.Cmd
	m.path, cmd = m.path.Update(k)
	return m.checkGit(), cmd
}

// export writes the entries of the current view, every status, to a new
// file at path. The developer's own memory: nothing is secret-scanned.
func (m Model) export(path string) tea.Cmd {
	st := m.d.Store
	pid, f := m.viewFilter()
	f.Statuses = allStatuses
	return func() tea.Msg {
		es, err := st.List(pid, f)
		if err != nil {
			return changedMsg{err: fmt.Errorf("nothing exported: %w", err)}
		}
		data, err := memory.ExportTOML(es)
		if err == nil {
			err = writeNew(path, data)
		}
		if errors.Is(err, fs.ErrExist) {
			return changedMsg{err: fmt.Errorf("%s exists; pick another name", path)}
		}
		if err != nil {
			return changedMsg{err: fmt.Errorf("nothing exported: %w", err)}
		}
		return changedMsg{status: fmt.Sprintf("exported %d entries to %s", len(es), path)}
	}
}

// writeNew creates path, private to the developer, and writes data to it. An
// existing path is never overwritten (fs.ErrExist); a failed write leaves no
// file behind. The open file is made 0600 through its handle (on Unix that
// is the whole job, and no path is followed); platform.SecureFile then gives
// it an owner-only DACL on Windows (on Unix it repeats the 0600).
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = f.Chmod(0o600)
	if err == nil {
		err = platform.SecureFile(path)
	}
	if err == nil {
		_, err = f.Write(data)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// readImport reads and parses the file I names, for the preview. It checks
// the path is a regular file before opening it (opening a FIFO with no
// writer would block for ever), and again through the handle in case the
// path changed in between.
func readImport(path string) tea.Cmd {
	return func() tea.Msg {
		notRegular := importReadMsg{err: fmt.Errorf("%s is not a regular file", path)}
		if fi, err := os.Stat(path); err != nil {
			return importReadMsg{err: err}
		} else if !fi.Mode().IsRegular() {
			return notRegular
		}
		f, err := os.Open(path)
		if err != nil {
			return importReadMsg{err: err}
		}
		defer f.Close()
		if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
			return notRegular
		}
		data, err := io.ReadAll(io.LimitReader(f, maxImportBytes+1))
		if err != nil {
			return importReadMsg{err: err}
		}
		if len(data) > maxImportBytes {
			return importReadMsg{err: fmt.Errorf("%s is larger than 4 MiB", path)}
		}
		es, err := memory.ImportTOML(data)
		if err != nil {
			return importReadMsg{err: err}
		}
		// Scanned here, off the UI goroutine: the preview names these
		// entries by number, never by text. The pack name (from) is
		// shown on the preview, so it is scanned too.
		var hits []int
		for i, e := range es {
			fromHit, _ := memory.ScanSecrets(e.From)
			if fromHit || len(memory.SecretHits(es[i:i+1])) > 0 {
				hits = append(hits, i+1)
			}
		}
		return importReadMsg{path: path, entries: es, hits: hits}
	}
}

// importRead opens the preview of a file I read, or says why not.
func (m Model) importRead(msg importReadMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeBrowse {
		return m, nil // the developer moved on
	}
	switch {
	case msg.err != nil:
		m.status = "nothing imported: " + clean(msg.err.Error())
	case len(msg.entries) == 0:
		m.status = "nothing imported: the file has no entries"
	case memory.NeedsProject(msg.entries) && m.d.Project.ID == "":
		m.status = "nothing imported: the file has project or dot memory and this screen has no project"
	default:
		m.mode, m.imp, m.impPath, m.impHits = modeImportPreview, msg.entries, msg.path, msg.hits
	}
	return m, nil
}

// hiddenPack stands for every pack name on a preview with secret-looking
// entries (pack names are valid slugs, so no real one is spelled this way).
const hiddenPack = "<hidden>"

// importGroup is one line of the preview's counts.
type importGroup struct {
	scope     memory.Scope
	dot, from string
	status    memory.Status
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (g importGroup) say(n int) string {
	noun := plural(n, "entry", "entries")
	switch g.status {
	case memory.StatusProposed:
		noun = plural(n, "proposal", "proposals")
	case memory.StatusActive:
		noun = "active " + noun
	case memory.StatusArchived:
		noun = "archived " + noun
	}
	var s string
	switch g.scope {
	case memory.ScopeProject:
		s = fmt.Sprintf("%d project %s", n, noun)
	case memory.ScopeGlobal:
		s = fmt.Sprintf("%d global %s for %s", n, noun, clean(g.dot))
	default:
		s = fmt.Sprintf("%d %s for %s", n, noun, clean(g.dot))
	}
	switch g.from {
	case "":
	case hiddenPack:
		s += " from pack " + hiddenPack
	default:
		s += fmt.Sprintf(" from pack %q", clean(g.from))
	}
	return s
}

// previewScreen is what an import would store: the file, the target, the
// counts by scope and status, and which entries look like secrets (by
// number only: their text may be a credential).
func (m Model) previewScreen(w int) []string {
	into := "global memory"
	if memory.NeedsProject(m.imp) {
		into = fmt.Sprintf("project %s (%s)", clean(m.d.Project.Name), clean(m.d.Project.ID))
	}
	var lines []string
	for _, l := range wrap("IMPORT MEMORY?", w) {
		lines = append(lines, style.Title.Render(l))
	}
	lines = append(lines, pathField("file: ", m.impPath, w)...)
	lines = append(lines, field("into: ", into, w)...)
	counts := map[importGroup]int{}
	var order []importGroup
	for _, e := range m.imp {
		g := importGroup{scope: e.Scope, dot: e.Dot, from: e.From, status: e.Status}
		if g.scope == memory.ScopeProject {
			g.dot = ""
		}
		if g.from != "" && len(m.impHits) > 0 {
			g.from = hiddenPack // a pack name may be the secret
		}
		if counts[g] == 0 {
			order = append(order, g)
		}
		counts[g]++
	}
	for _, g := range order {
		for _, l := range wrap(g.say(counts[g]), w-2) {
			lines = append(lines, "  "+l)
		}
	}
	if n := len(m.impHits); n > 0 {
		nums := make([]string, n)
		for i, h := range m.impHits {
			nums[i] = fmt.Sprint(h)
		}
		say := fmt.Sprintf("%d entries look like secrets: entries %s. Their text is not shown; press Y (capital) to import them anyway.", n, strings.Join(nums, ", "))
		if n == 1 {
			say = fmt.Sprintf("1 entry looks like a secret: entry %s. Its text is not shown; press Y (capital) to import it anyway.", nums[0])
		}
		lines = append(lines, wrap(say, w)...)
	}
	return lines
}

// keysImportPreview imports the previewed file on Y, or on y when no entry
// looks like a secret; any other key cancels.
func (m Model) keysImportPreview(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	fits, es, secret := m.shownWhole(), m.imp, len(m.impHits) > 0
	m.mode, m.imp, m.impPath, m.impHits = modeBrowse, nil, "", nil
	switch key := k.String(); {
	case key == "y" && secret:
		m.status = "nothing imported: entries look like secrets; press Y (capital) on the preview to import them"
		return m, nil
	case key != "y" && key != "Y": // Y always imports, y only without secrets
		m.status = "nothing imported"
		return m, nil
	case !fits:
		m.status = "nothing imported: the popup is too small to show the whole preview"
		return m, nil
	}
	pid := ""
	if memory.NeedsProject(es) {
		if pid = m.d.Project.ID; pid == "" {
			m.status = "nothing imported: the file has project or dot memory and this screen has no project"
			return m, nil
		}
	}
	return m, m.mutateSaying(func(st *memory.Store) (string, error) {
		res := st.ImportEntries(pid, es)
		say := fmt.Sprintf("imported %d entries", res.Added)
		if n := len(res.Skipped); n > 0 {
			say += fmt.Sprintf(" (%d skipped: %s)", n, res.Skipped[0])
		}
		return say, nil
	})
}

// countPurge counts the entries P would delete: every scope of this project
// (its own and its dots'), archived ones too.
func (m Model) countPurge() tea.Cmd {
	st, pid := m.d.Store, m.d.Project.ID
	return func() tea.Msg {
		n := 0
		for _, sc := range []memory.Scope{memory.ScopeProject, memory.ScopeDot} {
			es, err := st.List(pid, memory.Filter{Scope: sc, Statuses: allStatuses})
			if err != nil {
				return purgeCountMsg{n: -1}
			}
			n += len(es)
		}
		return purgeCountMsg{n: n}
	}
}

// purgeScreen names the project, says what goes and what stays, and asks for
// the project id.
func (m Model) purgeScreen(w int) []string {
	id := clean(m.d.Project.ID)
	goes := "Every entry of this project goes (some of its files could not be read), its dots' memory too."
	if m.purgeN >= 0 {
		goes = fmt.Sprintf("%d %s of this project go, its dots' memory too.", m.purgeN, plural(m.purgeN, "entry", "entries"))
	}
	var lines []string
	for _, l := range wrap("PURGE ALL MEMORY OF THIS PROJECT?", w) {
		lines = append(lines, style.Title.Render(l))
	}
	lines = append(lines, field("project: ", clean(m.d.Project.Name)+" ("+id+")", w)...)
	lines = append(lines, wrap(goes, w)...)
	lines = append(lines, wrap("Global memory is kept.", w)...)
	lines = append(lines, wrap(fmt.Sprintf("Type the project id %s and press enter:", id), w)...)
	lines = append(lines, m.path.View())
	return lines
}

// keysConfirmPurge deletes all of this project's memory on enter when the
// answer is exactly the project id; anything else keeps it.
func (m Model) keysConfirmPurge(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode, m.status = modeBrowse, "nothing purged"
		return m, nil
	case "enter":
		fits, id := m.shownWhole(), m.d.Project.ID
		m.mode = modeBrowse
		if !fits {
			m.status = "nothing purged: the popup is too small to show the whole confirmation"
			return m, nil
		}
		if m.path.Value() != id {
			m.status = fmt.Sprintf("nothing purged: type the project id %s to confirm", id)
			return m, nil
		}
		return m, m.mutate("purged "+id, func(st *memory.Store) error { return st.Purge(id) })
	}
	var cmd tea.Cmd
	m.path, cmd = m.path.Update(k)
	return m, cmd
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
		m.editRef, m.editPid, m.editSeen = &r, m.pidOf(*e), e.Text
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
		m.mode, m.status = modeBrowse, "" // an editor warning doesn't outlive the editor
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
			ref, pid, seen := *m.editRef, m.editPid, m.editSeen
			return m, m.mutateSaying(func(st *memory.Store) (string, error) {
				status := "saved " + ref.String()
				was := false
				got, err := st.Update(pid, ref, func(x *memory.Entry) error {
					was = x.Status == memory.StatusArchived
					// An import saved with its text unchanged stays someone
					// else's conclusion (the CLI rule, Task 10): it is an
					// approval of the text the editor showed, never a rewrite.
					if x.Source == memory.SourceImport && text == strings.TrimSpace(seen) {
						if x.Text != seen {
							return errChanged
						}
						x.Kind, x.Paths = kind, paths
						status = "saved (still imported) " + ref.String()
						if x.Status == memory.StatusProposed && x.Scope != memory.ScopeProject {
							x.Status = memory.StatusActive
							status = "approved (still imported) " + ref.String()
						}
						return nil
					}
					x.Text, x.Kind, x.Paths, x.Source = text, kind, paths, memory.SourceUser
					// Only imports propose dot memory (Phase 9); the developer's
					// own dot memory is active, so editing one approves it.
					if x.Status == memory.StatusProposed && x.Scope != memory.ScopeProject {
						x.Status = memory.StatusActive
						status = "saved and approved " + ref.String()
					}
					return nil
				})
				return archivedSay(status, got, was), err
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

// trunc cuts a row to n runes. row cleans every part it is built from, so
// trunc keeps its padding (clean would collapse the columns).
func trunc(s string, n int) string {
	r := []rune(textutil.SanitizeModelText(s))
	if len(r) > n {
		ell := style.Sym("…")
		return string(r[:max(1, n-utf8.RuneCountInString(ell))]) + ell
	}
	return string(r)
}

func (m Model) row(e memory.Entry) string {
	star := " "
	if e.Pinned {
		star = "★"
	}
	var who string
	text := clean(e.Text)
	switch {
	case e.Source == memory.SourceImport:
		who = "(imported)"
		if from := clean(e.From); from != "" {
			text = "from " + from + ": " + text // whose pack it came from
		}
	case e.Source == memory.SourceUser:
		who = "(you)"
	case e.Source == memory.SourceAgent:
		who = "(agent)"
	case e.Status == memory.StatusProposed || (e.Scope == memory.ScopeProject && e.Dot != ""):
		who = "(" + clean(e.Dot) + ")"
	default:
		who = fmt.Sprintf("used %d×", e.Uses)
	}
	return fmt.Sprintf("%-7s %s %-10s %-10s %s", clean(e.Ref().String()), star, clean(string(e.Kind)), who, text)
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
			d := style.Sym("─")
			lines = append(lines, style.Dim.Render(d+d+" "+title+" "+strings.Repeat(d, max(0, w-len(title)-4))))
			last = s
		}
		line := trunc(m.row(e), max(20, w-2))
		if i == m.cursor {
			cur = len(lines)
			lines = append(lines, style.Selected.Render(style.Sym("▸")+" "+line))
		} else {
			lines = append(lines, "  "+line)
		}
	}
	return lines, cur
}

// helpLines breaks the help line between its keys ("x forget", two spaces
// apart) into lines of at most w display cells, so no key is cut off.
func helpLines(help string, w int) []string {
	var lines []string
	cur := ""
	for _, item := range strings.Split(help, "  ") {
		switch {
		case cur == "":
			cur = item
		case ansi.StringWidth(cur+"  "+item) <= w:
			cur += "  " + item
		default:
			lines, cur = append(lines, cur), item
		}
	}
	return append(lines, cur)
}

func (m Model) View() string {
	w := m.screenWidth()
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
	head.WriteString(style.Title.Render(fmt.Sprintf("MEMORY → %s · %s %s %d entries", who, proj, style.Sym("─")+style.Sym("─"), len(m.entries))) + "\n")
	filter := ""
	if m.filter != "" {
		filter = "/" + clean(m.filter)
	}
	head.WriteString(fmt.Sprintf("Filter: %-30s scope: %s\n", filter, m.view()))
	if m.loadErr != "" {
		head.WriteString(style.Err.Render(clean(m.loadErr)) + "\n")
	}

	var tail strings.Builder
	ws, isWhole := m.wholeScreen(w)
	switch m.mode {
	case modeConfirmForget:
		tail.WriteString("\n" + style.Title.Render("Forget "+clean(m.forget.String())+"?") + " y forget · any other key keeps it\n")
	case modeConfirmForgetAll:
		sc, _, _, want := m.forgetAllTarget()
		tail.WriteString(fmt.Sprintf("\n%s Type %q and press enter:\n%s\n", style.Title.Render("Forget ALL "+string(sc)+" memory?"), want, m.input.View()))
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
				marker = style.Sym("▸") + " "
			}
			tail.WriteString(marker + label + " " + m.fields[i].View() + "\n")
		}
		tail.WriteString(style.Dim.Render("ctrl+s save · tab next field · esc cancel") + "\n")
	}
	if isWhole {
		if m.fits(ws.lines) {
			tail.WriteString("\n" + ws.prompt + "\n")
		} else {
			tail.WriteString("\n" + ws.cancel + "\n")
		}
	}
	footer := []string{"a approve  e edit  p pin  x forget  X forget all  n new  E export  I import  P purge  / filter  tab scope  esc back"}
	switch {
	case m.status != "" && isWhole: // one line, cut to the popup: approveTail counts it
		footer = []string{style.Cut(clean(m.status), w)}
	case m.status != "": // wrapped, so a long error neither overflows nor loses its end
		footer = wrap(m.status, w)
	case !isWhole: // a whole-popup screen keeps one line: approveTail counts it
		footer = helpLines(footer[0], w)
	}
	for i, l := range footer {
		if i > 0 {
			tail.WriteString("\n")
		}
		tail.WriteString(style.Dim.Render(l))
	}

	lines, cur := m.list(w)
	if isWhole {
		// A whole-popup screen (approve, export, import, purge) replaces the
		// header: it has its own title, and every line of the popup is
		// needed for what the developer confirms.
		head.Reset()
		lines, cur = ws.lines, 0
		if !m.fits(lines) {
			lines = m.tooSmall(ws.title, lines, ws.what, ws.action)
		}
	} else if len(m.rows) == 0 && m.loadErr == "" {
		lines = []string{style.Dim.Render("No memory here yet. Press n to add an entry.")}
	}
	// With a known height, scroll the list so the selected row stays visible.
	// A whole-popup screen is never cut: every word of a proposal must show.
	if m.height > 0 && !isWhole {
		used := strings.Count(head.String(), "\n") + strings.Count(tail.String(), "\n") + 1
		if avail := max(3, m.height-used); len(lines) > avail {
			start := max(0, min(cur-avail/2, len(lines)-avail))
			lines = lines[start : start+avail]
		}
	}
	return head.String() + strings.Join(lines, "\n") + "\n" + tail.String()
}
