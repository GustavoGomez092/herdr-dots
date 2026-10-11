package manage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// trustState is the trust preview of one project dot file, read by this
// popup: the hash sent with the trust command is the hash of exactly the
// bytes shown (raw), so trust binds what the developer saw. The engine
// refuses it if the file changed since.
type trustState struct {
	lib  view.LibraryDot
	dot  dots.Dot
	raw  []string // the file's bytes as visible lines (dots.VisibleLines), shown in full
	sum  string
	rel  string
	keep map[string]bool // per requested capability; all off at first
	pg   pager
}

type (
	trustOpenMsg struct {
		st  *trustState
		err error
	}
	trustDoneMsg struct {
		name    string
		trusted bool
	}
)

// errTooBig is readBounded's error for a file over its limit.
var errTooBig = errors.New("the file is too large")

// readBounded reads at most limit bytes of path and refuses a larger file
// without reading the rest.
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w (over %d KB)", errTooBig, limit>>10)
	}
	return data, nil
}

// projectRel is a project dot's file as shown: its path from the
// repository root, cleaned for the terminal.
func projectRel(d view.LibraryDot) string {
	if d.Rel != "" {
		return style.Clean(d.Rel)
	}
	base, _, _ := dots.SplitProjectSlug(d.Slug)
	return path.Join(".herdr-dots", "dots", base+".toml")
}

// openTrust reads the project dot file named in the snapshot once, hashes
// those bytes and parses them, off the UI goroutine.
func (m Model) openTrust(d view.LibraryDot) tea.Cmd {
	return func() tea.Msg {
		base, _, ok := dots.SplitProjectSlug(d.Slug)
		if !ok || d.Path == "" {
			return trustOpenMsg{err: fmt.Errorf("%s is not a project dot", style.Clean(d.Name))}
		}
		rel := projectRel(d)
		data, err := readBounded(d.Path, dots.MaxProjectFileBytes)
		if err != nil {
			return trustOpenMsg{err: fmt.Errorf("%s: %s", rel, cleanErr(err, d.Path, rel))}
		}
		parsed, err := dots.DecodeProject(dots.ProjectFile{Base: base, Path: d.Path, Rel: rel, Data: data}, d.ProjectID)
		if err != nil {
			return trustOpenMsg{err: fmt.Errorf("%s: %s", rel, cleanErr(err, d.Path, rel))}
		}
		return trustOpenMsg{st: &trustState{lib: d, dot: parsed, raw: dots.VisibleLines(data),
			sum: projectdots.Hex(projectdots.Sum(data)), rel: rel, keep: map[string]bool{}}}
	}
}

// cleanErr is an error for the status line: one clean line, the absolute
// path replaced by the file's path in its repository.
func cleanErr(err error, abs, rel string) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return style.Clean(pe.Op + " " + rel + ": " + pe.Err.Error())
	}
	return style.Clean(strings.ReplaceAll(err.Error(), abs, rel))
}

// kept lists the capabilities keep turns on, in display order.
func kept(keep map[string]bool) []string {
	var out []string
	for _, c := range dots.Capabilities {
		if keep[c] {
			out = append(out, c)
		}
	}
	return out
}

// grantsLine says what y would grant, always on screen under a preview: a
// toggle made while the capability list is scrolled away is never unseen.
func grantsLine(keep map[string]bool) string {
	ks := kept(keep)
	if len(ks) == 0 {
		return "grants: nothing (every capability it asks for stays off)"
	}
	return "grants: " + strings.Join(ks, ", ")
}

// capabilityLines lists the requested capabilities with their keep boxes
// and the clamps that can't be kept.
func capabilityLines(requested []string, all []dots.Clamp, keep map[string]bool) []string {
	var lines []string
	if len(requested) == 0 {
		lines = append(lines, "It asks for no risky capability.")
	} else {
		lines = append(lines, "CAPABILITIES it asks for (off unless you turn them on with the number):")
	}
	for i, c := range requested {
		mark := "[ ]"
		if keep[c] {
			mark = "[x]"
		}
		desc := c
		for _, cl := range all {
			if cl.Capability == c {
				desc = style.Clean(cl.String())
			}
		}
		lines = append(lines, fmt.Sprintf("  %s %d %s", mark, i+1, desc))
	}
	for _, cl := range all {
		if !cl.Keepable && !cl.Fact {
			lines = append(lines, "  always changed: "+style.Clean(cl.String()))
		}
	}
	return lines
}

// effectiveLines are the facts of the dot as it would run with the current
// keeps: backend kind, effective model and mode, and the budget.
func effectiveLines(written, eff dots.Dot) []string {
	model := style.Clean(eff.Backend.Model)
	if model == "" {
		model = "(default)"
	}
	usd := "off"
	if eff.Budget.MaxUSDPerSession > 0 {
		usd = "$" + strconv.FormatFloat(eff.Budget.MaxUSDPerSession, 'f', -1, 64)
	}
	lines := []string{
		fmt.Sprintf("backend:  %s %s model %s %s mode %s (as written: %s)", style.Clean(eff.Backend.Kind), style.Sym("·"), model, style.Sym("·"),
			style.Clean(string(eff.Delivery.Mode)), style.Clean(string(written.Delivery.Mode))),
		fmt.Sprintf("budget:   %d calls, %s per session", eff.Budget.MaxCallsPerSession, usd),
	}
	if eff.Backend.Effort != "" {
		lines = append(lines, "effort:   "+style.Clean(eff.Backend.Effort))
	}
	return lines
}

// fileRows shows one visible line of a file at width w: "  " and the line,
// wrapped by display cells with every byte kept (style.WrapExact), each
// continuation row starting with style.ContMark and a space.
func fileRows(l string, w int) []string {
	pieces := style.WrapExact(l, max(1, w-2))
	rows := make([]string, len(pieces))
	for i, p := range pieces {
		if i == 0 {
			rows[i] = "  " + p
		} else {
			rows[i] = style.ContMark + " " + p
		}
	}
	return rows
}

func (m Model) trustLines() []string {
	t, w := m.trust, m.bodyWidth()
	d := t.dot
	status := "untrusted"
	if t.lib.Trusted {
		status = "trusted"
		if t.lib.TrustedAt != "" {
			status += " (trusted at " + style.Clean(t.lib.TrustedAt) + ")"
		}
	}
	if t.lib.SHA256 != "" && t.lib.SHA256 != t.sum {
		status += "; " + style.Sym("⚠") + " this file differs from what the engine last read: esc, then T to read it again"
	}
	eff, _ := dots.ClampDot(d, kept(t.keep))
	_, clamps := dots.ClampDot(d, nil)
	lines := []string{
		style.Title.Render(fmt.Sprintf("TRUST %s (%s) from %s", style.Clean(dots.DisplaySlug(d.Slug)), style.Clean(d.Name), style.Clean(t.lib.ProjectName))),
		"file:     " + t.rel + " in " + style.Clean(t.lib.ProjectName),
		"sha256:   " + t.sum,
	}
	lines = append(lines, effectiveLines(d, eff)...)
	lines = append(lines, "status:   "+status, "", "PURPOSE (becomes this dot's system prompt):")
	for _, l := range style.Wrap(d.Purpose, w-2) {
		lines = append(lines, "  "+l)
	}
	lines = append(lines, "")
	lines = append(lines, capabilityLines(dots.Requested(d), clamps, t.keep)...)
	// Every byte is trusted, so every byte is shown: research topics, a
	// schedule task and its projects, backend args, and keys a later field
	// adds. Hidden characters show as escapes (dots.VisibleLines).
	// Wrapped here, byte for byte (fitLines leaves rows that fit alone): a
	// line starts with the two-space indent and each continuation row with
	// the marker, so a wrapped row never passes for a line of the file.
	lines = append(lines, "", "THE FILE AS WRITTEN (every byte you trust):")
	for _, l := range t.raw {
		lines = append(lines, fileRows(l, w)...)
	}
	lines = append(lines, "", "Any change to this file makes it untrusted again, and attached copies pause.")
	return m.fitLines(lines)
}

func (m Model) keysTrust(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.trust
	lines := m.trustLines()
	if t.pg.key(k.String(), len(lines), m.bodyHeight(), m.measured()) {
		return m, nil
	}
	requested := dots.Requested(t.dot)
	switch s := k.String(); s {
	case "esc", "q":
		m.scr, m.trust, m.status = scrLibrary, nil, ""
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i, _ := strconv.Atoi(s); i <= len(requested) {
			t.keep[requested[i-1]] = !t.keep[requested[i-1]]
		}
	case "y":
		if !m.measured() {
			m.status = waitingForSize
			return m, nil
		}
		if !t.pg.seenEnd {
			m.status = "scroll to the end of the file first (j or space)"
			return m, nil
		}
		args := ipc.TrustArgs{PaneID: m.target, Root: t.lib.Root, Slug: t.dot.Slug, SHA256: t.sum, Capabilities: kept(t.keep)}
		name := style.Clean(t.dot.Name)
		return m, func() tea.Msg {
			if err := m.call(ipc.CmdTrust, args); err != nil {
				return errMsg{err}
			}
			return trustDoneMsg{name: name, trusted: true}
		}
	case "u":
		if !t.lib.Trusted {
			return m, nil
		}
		args := ipc.TrustArgs{PaneID: m.target, Root: t.lib.Root, Slug: t.dot.Slug}
		name := style.Clean(t.dot.Name)
		return m, func() tea.Msg {
			if err := m.call(ipc.CmdUntrust, args); err != nil {
				return errMsg{err}
			}
			return trustDoneMsg{name: name}
		}
	}
	return m, nil
}

func (m Model) trustView() string {
	lines := m.trustLines()
	var b strings.Builder
	b.WriteString(strings.Join(m.trust.pg.window(lines, m.bodyHeight()), "\n") + "\n")
	if m.status != "" {
		b.WriteString(m.cutLine(style.Clean(m.status)) + "\n")
	}
	b.WriteString(m.cutLine(grantsLine(m.trust.keep)) + "\n")
	sep := " " + style.Sym("·") + " "
	help := strings.Join([]string{"j/k scroll", "1-9 toggle a capability", "y trust", "esc back"}, sep)
	if m.trust.lib.Trusted {
		help += sep + "u untrust"
	}
	b.WriteString(style.Dim.Render(m.cutLine(help)))
	return b.String()
}
