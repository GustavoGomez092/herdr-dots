// Package diff is the patch viewer: a file list and a unified-diff pager for
// one drafted patch, and the apply confirm (gate 2), which always happens
// with the diff on screen.
//
// Everything shown is display-only text: terminal control sequences and
// bidi/format characters are removed before it reaches the screen, but the
// patch the engine applies is never altered.
package diff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

// largeLines is the diff length from which a file is flagged "large".
const largeLines = 2000

// File is one file's section of a patch.
type File struct {
	Path      string
	Lines     []string // from its "diff --git" line on, display-safe
	Hunks     []int    // indexes into Lines of the "@@" lines
	Binary    bool
	Added     int
	Deleted   int
	Exec      bool // gains the executable bit (new file or mode change)
	Deletion  bool // the file is removed
	New       bool // the file is created
	Large     bool
	Symlink   bool // mode 120000
	Submodule bool // mode 160000
	Renamed   bool // renamed: the old path is removed
	Copied    bool
}

// clean makes model- or repository-controlled text safe to display on one
// line: control characters (including ESC, so no escape sequence survives),
// tabs, and bidi/format/line-separator characters (U+202A-202E, U+2066-2069,
// U+200B-200F, U+FEFF and the rest of Cf, Zl, Zp) are removed or replaced.
// Display only.
func clean(s string) string {
	s = textutil.SanitizeModelText(s)
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Cc, unicode.Cs, unicode.Co) {
			return -1
		}
		return r
	}, s)
}

// Parse splits `git diff` output into files. Header lines are recognised on
// the raw line (a zero-width prefix does not make a file, so the file count
// is what git would apply); lines kept for display are sanitized because
// file contents came from a model and must not reach the terminal raw.
func Parse(patch []byte) []File {
	var files []File
	for _, raw := range strings.Split(strings.TrimRight(string(patch), "\n"), "\n") {
		if strings.HasPrefix(raw, "diff --git ") {
			files = append(files, File{Path: clean(pathOf(raw))})
		}
		if len(files) == 0 {
			continue
		}
		f := &files[len(files)-1]
		inHeader := len(f.Hunks) == 0 && !f.Binary
		switch {
		case strings.HasPrefix(raw, "@@"):
			f.Hunks = append(f.Hunks, len(f.Lines))
		case strings.HasPrefix(raw, "GIT binary patch"), strings.HasPrefix(raw, "Binary files "):
			f.Binary = true
		case inHeader && strings.HasPrefix(raw, "new file mode "):
			f.New = true
			f.noteMode(strings.TrimPrefix(raw, "new file mode "))
		case inHeader && strings.HasPrefix(raw, "new mode "):
			f.noteMode(strings.TrimPrefix(raw, "new mode "))
		case inHeader && strings.HasPrefix(raw, "deleted file mode "):
			f.Deletion = true
			f.noteMode(strings.TrimPrefix(raw, "deleted file mode "))
		case inHeader && strings.HasPrefix(raw, "index "):
			if fl := strings.Fields(raw); len(fl) == 3 {
				f.noteMode(fl[2])
			}
		case inHeader && strings.HasPrefix(raw, "rename from "):
			f.Renamed, f.Deletion = true, true
		case inHeader && strings.HasPrefix(raw, "copy from "):
			f.Copied = true
		case strings.HasPrefix(raw, "+") && !strings.HasPrefix(raw, "+++ "):
			f.Added++
		case strings.HasPrefix(raw, "-") && !strings.HasPrefix(raw, "--- "):
			f.Deleted++
		}
		f.Lines = append(f.Lines, clean(raw))
	}
	for i := range files {
		files[i].Large = len(files[i].Lines) > largeLines
	}
	return files
}

// noteMode flags a mode: symlink, submodule, or any executable bit.
func (f *File) noteMode(mode string) {
	v, err := strconv.ParseUint(strings.TrimSpace(mode), 8, 32)
	if err != nil {
		return
	}
	switch v &^ 0o7777 {
	case 0o120000:
		f.Symlink = true
	case 0o160000:
		f.Submodule = true
	case 0o100000:
		if v&0o111 != 0 && !f.Deletion {
			f.Exec = true
		}
	}
}

// pathOf reads the b/ path of a "diff --git a/x b/x" line.
func pathOf(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return rest
}

type appliedMsg struct{ err error }

// Model is the viewer.
type Model struct {
	files   []File
	cov     [][]bool // per file, per source line: every row of it has been on screen
	part    [][]bool // per file, per row at width partW: has been on screen
	partW   int
	wc      *wrapped // the selected file wrapped to the content width
	sized   bool     // a WindowSizeMsg arrived
	meta    patchstore.Meta
	cardID  string
	sum     string // SHA-256 of the exact patch bytes shown
	caller  Caller
	file    int // selected file
	top     int // first diff row shown (rows are wrapped source lines)
	width   int
	height  int
	confirm bool // the apply question is open
	busy    bool
	applied bool
	status  string
	failed  error // the patch could not be loaded
}

// New shows patch (meta describes it) for the patch card cardID.
func New(patch []byte, meta patchstore.Meta, cardID string, c Caller) Model {
	files := Parse(patch)
	if len(meta.Paths) == len(files) { // git's own path list beats parsing quoted headers
		for i := range files {
			files[i].Path = clean(meta.Paths[i])
		}
	}
	for i := range files {
		files[i].Path = clean(files[i].Path)
	}
	h := sha256.Sum256(patch)
	return Model{files: files, meta: meta, cardID: cardID, caller: c, sum: hex.EncodeToString(h[:])}
}

// Failed is a viewer that only explains why the patch can't be shown.
func Failed(err error) Model { return Model{failed: err} }

// Arm opens the apply question right away (the Dots pane's A key).
func (m Model) Arm() Model {
	if m.failed == nil && m.cardID != "" && len(m.files) > 0 {
		m.confirm = true
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// markSeen records the rows of the selected file that are on screen now. A
// source line counts as seen once every one of its wrapped rows has been on
// screen at the current width (a resize starts partial lines over). Nothing
// is recorded before the window size is known or in a too-narrow column. It
// copies what it changes: models are values and may be forked.
func (m Model) markSeen() Model {
	m.reflow()
	if !m.sized || m.file < 0 || m.file >= len(m.files) || m.contentWidth() < minContent {
		return m
	}
	wr := m.wc
	from, to := m.top, min(len(wr.rows), m.top+m.pageLines())
	if from < 0 || from >= to {
		return m
	}
	part := make([][]bool, len(m.files))
	if m.partW == wr.width {
		copy(part, m.part)
	}
	pr := make([]bool, len(wr.rows))
	copy(pr, part[m.file])
	for i := from; i < to; i++ {
		pr[i] = true
	}
	part[m.file] = pr
	m.part, m.partW = part, wr.width

	cov := make([][]bool, len(m.files))
	copy(cov, m.cov)
	seen := make([]bool, len(m.files[m.file].Lines))
	copy(seen, cov[m.file])
	for src := wr.rows[from].src; src <= wr.rows[to-1].src; src++ {
		all := true
		for r := wr.start[src]; r < wr.start[src+1]; r++ {
			if !pr[r] {
				all = false
				break
			}
		}
		if all {
			seen[src] = true
		}
	}
	cov[m.file] = seen
	m.cov = cov
	return m
}

// unseen lists the files with at least one line that never was on screen.
func (m Model) unseen() []string {
	var out []string
	for i, f := range m.files {
		ok := i < len(m.cov) && len(m.cov[i]) == len(f.Lines)
		if ok {
			for _, v := range m.cov[i] {
				if !v {
					ok = false
					break
				}
			}
		}
		if !ok {
			out = append(out, f.Path)
		}
	}
	return out
}

func (m Model) w() int {
	if m.width <= 0 {
		return 100
	}
	return m.width
}

func (m Model) summaryLine() string {
	s := strings.TrimSpace(clean(credentials.Redact(m.meta.Summary)))
	if s == "" {
		return ""
	}
	return fit(s, m.w())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// includes names the risky kinds of change in the patch.
func (m Model) includes() string {
	var bin, del, exe, large, sym, sub, ren int
	for _, f := range m.files {
		if f.Binary {
			bin++
		}
		if f.Deletion {
			del++
		}
		if f.Exec {
			exe++
		}
		if f.Large {
			large++
		}
		if f.Symlink {
			sym++
		}
		if f.Submodule {
			sub++
		}
		if f.Renamed || f.Copied {
			ren++
		}
	}
	var parts []string
	if bin > 0 {
		parts = append(parts, fmt.Sprintf("%d binary", bin))
	}
	if del > 0 {
		parts = append(parts, plural(del, "deletion", "deletions"))
	}
	if exe > 0 {
		parts = append(parts, fmt.Sprintf("%d executable", exe))
	}
	if large > 0 {
		parts = append(parts, fmt.Sprintf("%d large", large))
	}
	if sym > 0 {
		parts = append(parts, fmt.Sprintf("%d symlink", sym))
	}
	if sub > 0 {
		parts = append(parts, fmt.Sprintf("%d submodule", sub))
	}
	if ren > 0 {
		parts = append(parts, fmt.Sprintf("%d renamed/copied", ren))
	}
	return strings.Join(parts, ", ")
}

// confirmLines is the apply question with what it covers.
func (m Model) confirmLines() []string {
	w := m.w()
	n := len(m.files)
	noun := "files"
	if n == 1 {
		noun = "file"
	}
	text := style.Title.Render(fmt.Sprintf("Apply %d %s to your working tree? Nothing is staged or committed. (y/N)", n, noun))
	out := strings.Split(ansi.Wrap(text, w, ""), "\n")
	if inc := m.includes(); inc != "" {
		out = append(out, strings.Split(ansi.Wrap("Includes: "+inc, w, ""), "\n")...)
	}
	if u := m.unseen(); len(u) > 0 {
		shown := u
		if len(shown) > 3 {
			shown = shown[:3]
		}
		s := "Not yet viewed: " + strings.Join(shown, ", ")
		if len(u) > len(shown) {
			s += fmt.Sprintf(" and %d more", len(u)-len(shown))
		}
		out = append(out, strings.Split(ansi.Wrap(s, w, ""), "\n")...)
	}
	return out
}

const tooSmall = "window too small — enlarge to review"

// minDiffLines is how much diff must stay on screen beside the apply question.
const minDiffLines = 3

// fits reports whether the header, the file line, the whole apply question
// and at least minDiffLines of diff fit the window, in a diff column wide
// enough to wrap into. The question is only ever open (and y only ever
// acts) when they do.
func (m Model) fits() bool {
	if !m.sized || m.contentWidth() < minContent {
		return false
	}
	narrow := 0
	if m.w() < 60 {
		narrow = 1
	}
	return m.height >= 1+narrow+len(m.confirmLines())+minDiffLines
}

// footer is the bottom of the screen: the apply question, or the
// status/help line. Lines are wrapped to the width.
func (m Model) footer() []string {
	w := m.w()
	switch {
	case m.confirm:
		return m.confirmLines()
	case m.status != "":
		return strings.Split(ansi.Wrap(style.Dim.Render(m.status), w, ""), "\n")
	}
	return []string{style.Dim.Render(fit("j/k file · ↑/↓ scroll · n/p hunk · A apply · q close", w))}
}

// layout decides whether the summary line fits and how many diff lines
// there are: the window minus the header, the summary, the footer and (in
// the narrow layout) the file line. The summary is the first thing dropped.
func (m Model) layout() (summary bool, page int) {
	has := m.summaryLine() != ""
	if m.height <= 0 {
		return has, 20
	}
	used := 1 + len(m.footer())
	if m.w() < 60 {
		used++
	}
	if has && m.height-used-1 >= minDiffLines {
		used++
		summary = true
	}
	return summary, max(0, m.height-used)
}

func (m Model) pageLines() int {
	_, p := m.layout()
	return p
}

func (m Model) cur() File {
	if m.file >= 0 && m.file < len(m.files) {
		return m.files[m.file]
	}
	return File{}
}

// scroll moves by d rows.
func (m *Model) scroll(d int) {
	m.reflow()
	m.top = max(0, min(m.top+d, len(m.wc.rows)-1))
}

func (m *Model) selectFile(i int) {
	if len(m.files) == 0 {
		return
	}
	m.file = max(0, min(i, len(m.files)-1))
	m.top = 0
	m.reflow()
}

// hunk moves to the next (d=1) or previous (d=-1) hunk, crossing files.
func (m *Model) hunk(d int) {
	m.reflow()
	f := m.cur()
	if d > 0 {
		for _, h := range f.Hunks {
			if r := m.wc.start[h]; r > m.top {
				m.top = r
				return
			}
		}
		if m.file+1 < len(m.files) {
			m.selectFile(m.file + 1)
			if hs := m.cur().Hunks; len(hs) > 0 {
				m.top = m.wc.start[hs[0]]
			}
		}
		return
	}
	for i := len(f.Hunks) - 1; i >= 0; i-- {
		if r := m.wc.start[f.Hunks[i]]; r < m.top {
			m.top = r
			return
		}
	}
	if m.file > 0 {
		m.selectFile(m.file - 1)
		if hs := m.cur().Hunks; len(hs) > 0 {
			m.top = m.wc.start[hs[len(hs)-1]]
		}
	}
}

func (m Model) apply() tea.Cmd {
	c, id, sum := m.caller, m.cardID, m.sum
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return appliedMsg{err: c.Call(ctx, ipc.CmdApplyPatch, ipc.CardArgs{ID: id, SHA256: sum})}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		src := m.topLine() // keep the same source line at the top
		m.width, m.height, m.sized = msg.Width, msg.Height, true
		m.reflow()
		if src < len(m.wc.start)-1 {
			m.top = m.wc.start[src]
		}
		if m.confirm && !m.fits() {
			m.confirm, m.status = false, tooSmall
		}
		return m.markSeen(), nil
	case appliedMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "not applied: " + clean(msg.err.Error())
			return m, nil
		}
		m.applied = true
		m.status = "applied to your working tree (nothing staged or committed); conflicts, if any, are in the Dots feed · q close"
		return m, nil
	case tea.KeyMsg:
		nm, cmd := m.keys(msg)
		return nm.markSeen(), cmd
	}
	return m, nil
}

func (m Model) keys(k tea.KeyMsg) (Model, tea.Cmd) {
	if m.failed != nil {
		return m, tea.Quit
	}
	if k.String() == "ctrl+c" { // close even mid-apply; the engine finishes the apply
		return m, tea.Quit
	}
	if m.busy {
		return m, nil
	}
	if m.confirm {
		if !m.fits() { // never act on a question that isn't fully on screen
			m.confirm, m.status = false, tooSmall
			return m, nil
		}
		m.confirm = false
		if k.String() == "y" && m.caller != nil {
			m.busy, m.status = true, "applying…"
			return m, m.apply()
		}
		m.status = "not applied"
		return m, nil
	}
	switch k.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "j":
		m.selectFile(m.file + 1)
	case "k":
		m.selectFile(m.file - 1)
	case "down":
		m.scroll(1)
	case "up":
		m.scroll(-1)
	case "pgdown", "ctrl+d", " ":
		m.scroll(m.pageLines())
	case "pgup", "ctrl+u":
		m.scroll(-m.pageLines())
	case "n":
		m.hunk(1)
	case "p":
		m.hunk(-1)
	case "A":
		switch {
		case m.applied:
			m.status = "already applied"
		case m.cardID == "":
			m.status = "this patch has no card to apply"
		case len(m.files) == 0:
			m.status = "this patch has no files"
		case !m.fits():
			m.status = tooSmall
		default:
			m.confirm = true
		}
	}
	return m, nil
}

var (
	addStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	delStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	hunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// colorRow colours one row (text) by its source line (src), so continuation
// rows keep the +/- colour.
func colorRow(src, text string) string {
	switch {
	case strings.HasPrefix(src, "@@"):
		return hunkStyle.Render(text)
	case strings.HasPrefix(src, "+++ "), strings.HasPrefix(src, "--- "), strings.HasPrefix(src, "diff --git "):
		return style.Title.Render(text)
	case strings.HasPrefix(src, "+"):
		return addStyle.Render(text)
	case strings.HasPrefix(src, "-"):
		return delStyle.Render(text)
	}
	return text
}

// fit cuts s to w terminal cells, marking the cut with an ellipsis.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func flagTags(f File) string {
	var s string
	if f.Exec {
		s += " [exec]"
	}
	if f.Deletion {
		s += " [deleted]"
	}
	if f.New {
		s += " [new]"
	}
	if f.Symlink {
		s += " [symlink]"
	}
	if f.Submodule {
		s += " [submodule]"
	}
	if f.Renamed {
		s += " [renamed]"
	}
	if f.Copied {
		s += " [copied]"
	}
	if f.Large {
		s += " [large]"
	}
	return s
}

func (m Model) View() string {
	if m.failed != nil {
		return style.Err.Render("can't show this patch: "+clean(m.failed.Error())) + "\n\n" + style.Dim.Render("any key closes")
	}
	w := m.w()
	var b strings.Builder
	head := fmt.Sprintf("PATCH  %s · %s", clean(m.meta.Dot), clean(m.meta.Stat))
	if len(m.meta.Flags) > 0 {
		flags := make([]string, len(m.meta.Flags))
		for i, f := range m.meta.Flags {
			flags[i] = clean(f)
		}
		head += "  [" + strings.Join(flags, "] [") + "]"
	}
	b.WriteString(style.Title.Render(fit(head, w)) + "\n")
	summary, page := m.layout()
	if summary {
		b.WriteString(m.summaryLine() + "\n")
	}
	left := m.leftWidth()
	var list []string
	for i, f := range m.files {
		stat := "+" + strconv.Itoa(f.Added) + " −" + strconv.Itoa(f.Deleted)
		if f.Binary {
			stat = "binary"
		}
		suffix := " " + stat + flagTags(f)
		line := fit(f.Path, max(1, left-2-ansi.StringWidth(suffix))) + suffix
		if i == m.file {
			line = style.Selected.Render(fit("▸ "+line, left))
		} else {
			line = fit("  "+line, left)
		}
		list = append(list, line)
	}
	if len(list) > page {
		off := max(0, min(m.file-page+1, len(list)-page))
		list = list[off : off+page]
	}
	// diff rows are wrapped to the column, never cut (wrap.go)
	wr := m.wrap()
	src := m.cur().Lines
	end := min(len(wr.rows), m.top+page)
	var shown []string
	for _, r := range wr.rows[min(max(0, m.top), end):end] {
		shown = append(shown, colorRow(src[r.src], r.text))
	}
	if w < 60 { // too narrow for two columns: the selected file's name, then its diff
		b.WriteString(style.Dim.Render(fit(fmt.Sprintf("file %d/%d: %s%s", m.file+1, len(m.files), m.cur().Path, flagTags(m.cur())), w)) + "\n")
		if len(shown) > 0 {
			b.WriteString(strings.Join(shown, "\n") + "\n")
		}
	} else if page > 0 {
		lw := lipgloss.NewStyle().Width(left).Render(strings.Join(list, "\n"))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, lw, " │ ", strings.Join(shown, "\n")) + "\n")
	}
	b.WriteString(strings.Join(m.footer(), "\n"))
	out := b.String()
	if m.sized && m.height > 0 { // a status longer than a tiny window: keep the top
		if ls := strings.Split(out, "\n"); len(ls) > m.height {
			out = strings.Join(ls[:m.height], "\n")
		}
	}
	return out
}
