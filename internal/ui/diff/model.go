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
	Path     string
	Lines    []string // from its "diff --git" line on, display-safe
	Hunks    []int    // indexes into Lines of the "@@" lines
	Binary   bool
	Added    int
	Deleted  int
	Exec     bool // gains the executable bit (new file or mode change)
	Deletion bool // the file is removed
	New      bool // the file is created
	Large    bool
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

// Parse splits `git diff` output into files. Every line is sanitized: file
// contents came from a model and must not reach the terminal raw.
func Parse(patch []byte) []File {
	var files []File
	for _, raw := range strings.Split(strings.TrimRight(string(patch), "\n"), "\n") {
		line := clean(raw)
		if strings.HasPrefix(line, "diff --git ") {
			files = append(files, File{Path: pathOf(line)})
		}
		if len(files) == 0 {
			continue
		}
		f := &files[len(files)-1]
		switch {
		case strings.HasPrefix(line, "@@"):
			f.Hunks = append(f.Hunks, len(f.Lines))
		case strings.HasPrefix(line, "GIT binary patch"), strings.HasPrefix(line, "Binary files "):
			f.Binary = true
		case strings.HasPrefix(line, "new file mode "):
			f.New = true
			f.Exec = f.Exec || strings.HasSuffix(line, "755")
		case strings.HasPrefix(line, "new mode "):
			f.Exec = f.Exec || strings.HasSuffix(line, "755")
		case strings.HasPrefix(line, "deleted file mode "):
			f.Deletion = true
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ "):
			f.Added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- "):
			f.Deleted++
		}
		f.Lines = append(f.Lines, line)
	}
	for i := range files {
		files[i].Large = len(files[i].Lines) > largeLines
	}
	return files
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
	seen    []bool // per file: its whole diff has been on screen
	meta    patchstore.Meta
	cardID  string
	sum     string // SHA-256 of the exact patch bytes shown
	caller  Caller
	file    int // selected file
	top     int // first diff line shown
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
	return Model{files: files, meta: meta, cardID: cardID, caller: c, sum: hex.EncodeToString(h[:])}.markSeen()
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

// markSeen records the selected file as viewed once its last line is on
// screen. It copies the slice: models are values and may be forked.
func (m Model) markSeen() Model {
	if m.file < 0 || m.file >= len(m.files) {
		return m
	}
	if m.top+m.pageLines() < len(m.files[m.file].Lines) {
		return m
	}
	seen := make([]bool, len(m.files))
	copy(seen, m.seen)
	seen[m.file] = true
	m.seen = seen
	return m
}

func (m Model) unseen() []string {
	var out []string
	for i, f := range m.files {
		if i >= len(m.seen) || !m.seen[i] {
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
	s := strings.TrimSpace(clean(m.meta.Summary))
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
	var bin, del, exe, large int
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
	return strings.Join(parts, ", ")
}

// footer is the bottom of the screen: the apply question with what it
// covers, or the status/help line. Lines are wrapped to the width.
func (m Model) footer() []string {
	w := m.w()
	switch {
	case m.confirm:
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
	case m.status != "":
		return strings.Split(ansi.Wrap(style.Dim.Render(m.status), w, ""), "\n")
	}
	return []string{style.Dim.Render(fit("j/k file · ↑/↓ scroll · n/p hunk · A apply · q close", w))}
}

// pageLines is how many diff lines fit: the window minus the header, the
// summary, the footer and (in the narrow layout) the file line.
func (m Model) pageLines() int {
	if m.height <= 0 {
		return 20
	}
	used := 1 + len(m.footer())
	if m.summaryLine() != "" {
		used++
	}
	if m.w() < 60 {
		used++
	}
	return max(1, m.height-used)
}

func (m Model) cur() File {
	if m.file >= 0 && m.file < len(m.files) {
		return m.files[m.file]
	}
	return File{}
}

func (m *Model) scroll(d int) {
	m.top = max(0, min(m.top+d, len(m.cur().Lines)-1))
}

func (m *Model) selectFile(i int) {
	if len(m.files) == 0 {
		return
	}
	m.file = max(0, min(i, len(m.files)-1))
	m.top = 0
}

// hunk moves to the next (d=1) or previous (d=-1) hunk, crossing files.
func (m *Model) hunk(d int) {
	f := m.cur()
	if d > 0 {
		for _, h := range f.Hunks {
			if h > m.top {
				m.top = h
				return
			}
		}
		if m.file+1 < len(m.files) {
			m.selectFile(m.file + 1)
			if hs := m.cur().Hunks; len(hs) > 0 {
				m.top = hs[0]
			}
		}
		return
	}
	for i := len(f.Hunks) - 1; i >= 0; i-- {
		if f.Hunks[i] < m.top {
			m.top = f.Hunks[i]
			return
		}
	}
	if m.file > 0 {
		m.selectFile(m.file - 1)
		if hs := m.cur().Hunks; len(hs) > 0 {
			m.top = hs[len(hs)-1]
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
		m.width, m.height = msg.Width, msg.Height
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
	if m.busy {
		return m, nil
	}
	if m.confirm {
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

func colorLine(l string) string {
	switch {
	case strings.HasPrefix(l, "@@"):
		return hunkStyle.Render(l)
	case strings.HasPrefix(l, "+++ "), strings.HasPrefix(l, "--- "), strings.HasPrefix(l, "diff --git "):
		return style.Title.Render(l)
	case strings.HasPrefix(l, "+"):
		return addStyle.Render(l)
	case strings.HasPrefix(l, "-"):
		return delStyle.Render(l)
	}
	return l
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
	if s := m.summaryLine(); s != "" {
		b.WriteString(s + "\n")
	}
	page := m.pageLines()
	left := min(44, max(14, w/3))
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
	lines := m.cur().Lines
	end := min(len(lines), m.top+page)
	var shown []string
	if w < 60 { // too narrow for two columns: the selected file's name, then its diff
		for _, l := range lines[min(m.top, end):end] {
			shown = append(shown, colorLine(fit(l, w)))
		}
		b.WriteString(style.Dim.Render(fit(fmt.Sprintf("file %d/%d: %s%s", m.file+1, len(m.files), m.cur().Path, flagTags(m.cur())), w)) + "\n")
		b.WriteString(strings.Join(shown, "\n") + "\n")
	} else {
		for _, l := range lines[min(m.top, end):end] {
			shown = append(shown, colorLine(fit(l, max(1, w-left-3))))
		}
		lw := lipgloss.NewStyle().Width(left).Render(strings.Join(list, "\n"))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, lw, " │ ", strings.Join(shown, "\n")) + "\n")
	}
	b.WriteString(strings.Join(m.footer(), "\n"))
	return b.String()
}
