package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/profile"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/version"
	"herdr-dots/internal/view"
)

// ---- profile

type profileOpenMsg struct {
	deps profile.Deps
	err  error
}

// openProfile loads the dot's purpose (from its file) off the UI goroutine.
// The card bodies the profile may show are copied from the snapshot now, so
// its lookup is a map read: no engine call, no I/O in View.
func (m Model) openProfile(d view.LibraryDot) tea.Cmd {
	dir, hs, st, session := m.d.DotsDir, m.d.History, m.d.Memory, m.d.SessionKey
	cards := map[string]string{}
	for _, p := range m.snap.Panes {
		for _, c := range p.Cards {
			if k := history.CardKey(session, c.ID); k != "" {
				cards[k] = c.Body
			}
		}
	}
	return func() tea.Msg {
		var full dots.Dot
		var err error
		if d.Origin == dots.OriginProject {
			var data []byte
			if data, err = readBounded(d.Path, dots.MaxProjectFileBytes); err == nil {
				full, err = dots.DecodeStrict(data)
			}
			if err != nil {
				err = fmt.Errorf("%s: %s", projectRel(d), cleanErr(err, d.Path, projectRel(d)))
			}
		} else {
			full, err = dots.Load(dir, d.Slug)
		}
		if err != nil {
			return profileOpenMsg{err: err}
		}
		body := func(key string) (string, bool) { b, ok := cards[key]; return b, ok }
		return profileOpenMsg{deps: profile.Deps{History: hs, Memory: st, Dot: d, Purpose: full.Purpose, CardBody: body}}
	}
}

// ---- import

// importState is the import preview of one parsed pack.
type importState struct {
	pack    dotpack.Pack
	from    string
	name    string          // what it will be saved as
	keep    map[string]bool // capabilities to keep; all off at first
	project memory.Project  // where its memory goes ("" = nowhere)
	memNote string          // why memory isn't imported, if it isn't
	withMem bool            // import the pack's memory
	taken   bool            // a dot with this name exists
	pg      pager
}

type (
	importLoadedMsg struct {
		st  *importState
		err error
	}
	importedMsg struct {
		name, slug string
		res        memory.ApplyResult
		memErr     error
	}
)

func newPrompt(placeholder, value string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 2048
	ti.Width = 60
	ti.SetValue(value)
	ti.Focus()
	return ti
}

func expandHome(p string) string {
	for _, prefix := range []string{"~/", "~" + string(filepath.Separator)} {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			if home, err := os.UserHomeDir(); err == nil {
				return filepath.Join(home, rest)
			}
		}
	}
	return p
}

// shortHome abbreviates the home directory in a path shown to the developer.
func shortHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, filepath.Clean(home)+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}

// dotTaken reports whether a dot file with name's slug exists.
func dotTaken(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, dots.Slugify(name)+".toml"))
	return err == nil
}

// loadPack reads a pack file or URL, parses it and works out where its memory
// would go, off the UI goroutine.
func (m Model) loadPack(src string) tea.Cmd {
	fetch, dir, st, target, resolve := m.d.Fetch, m.d.DotsDir, m.d.Memory, m.target, m.d.Project
	pv, _ := m.snap.Pane(m.target)
	return func() tea.Msg {
		var data []byte
		var from string
		var err error
		if strings.Contains(src, "://") {
			if fetch == nil {
				return importLoadedMsg{err: errors.New("downloads are not available here")}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			data, from, err = fetch(ctx, src)
		} else {
			from = expandHome(src)
			if data, err = readBounded(from, dotpack.MaxBytes); errors.Is(err, errTooBig) {
				err = dotpack.ErrTooLarge
			}
		}
		if err != nil {
			return importLoadedMsg{err: err}
		}
		p, err := dotpack.Parse(data)
		if err != nil {
			return importLoadedMsg{err: err}
		}
		is := &importState{pack: p, from: from, name: p.Dot.Name, keep: map[string]bool{}}
		is.taken = dotTaken(dir, is.name)
		switch {
		case len(p.Memory) == 0:
		case st == nil:
			is.memNote = "memory is off"
		default:
			pr := memory.Project{ID: pv.ProjectID, Name: pv.ProjectName}
			if pr.ID == "" && target != "" && resolve != nil {
				pr, _ = resolve(target)
			}
			if pr.ID == "" {
				is.memNote = "no project: focus an agent pane in the repository first"
			} else {
				is.project, is.withMem = pr, true
			}
		}
		return importLoadedMsg{st: is}
	}
}

// prepared is the dot the import would save with the current name and
// keeps (dotpack.Prepare), and the clamps it applies.
func (is *importState) prepared() (dots.Dot, []dots.Clamp, error) {
	p := is.pack
	p.Dot.Name = is.name
	return dotpack.Prepare(p, kept(is.keep))
}

func (m Model) importLines() []string {
	is, w := m.imp, m.bodyWidth()
	p := is.pack
	written := p.Dot
	eff, _, perr := is.prepared()
	_, clamps := dots.ClampDot(written, nil)
	lines := []string{style.Title.Render("IMPORT " + style.Clean(is.name) + " from " + style.Clean(shortHome(is.from)))}
	if p.Meta.Note != "" {
		lines = append(lines, "note:     "+style.Clean(p.Meta.Note))
	}
	lines = append(lines,
		fmt.Sprintf("exported: %s by herdr-dots %s", p.Meta.ExportedAt.Format("2006-01-02"), style.Clean(p.Meta.Version)),
		fmt.Sprintf("saves as: %s (%s)", style.Clean(is.name), dots.Slugify(is.name)))
	if is.taken {
		lines = append(lines, style.Err.Render(fmt.Sprintf("%s a dot named %q exists: press n to choose another name (nothing is overwritten)", style.Sym("⚠"), style.Clean(is.name))))
	}
	if perr != nil {
		lines = append(lines, style.Err.Render(style.Sym("⚠")+" it can't be imported as it is: "+style.Clean(perr.Error())))
		eff, _ = dots.ClampDot(written, kept(is.keep))
	}
	lines = append(lines, effectiveLines(written, eff)...)
	lines = append(lines, "", "PURPOSE (becomes this dot's system prompt):")
	for _, l := range style.Wrap(written.Purpose, w-2) {
		lines = append(lines, "  "+l)
	}
	// Phase 8 text reaches a prompt once research or a schedule is kept.
	if len(written.Research.Topics) > 0 {
		lines = append(lines, "", "RESEARCH TOPICS:")
		for _, tp := range written.Research.Topics {
			for i, l := range style.Wrap(tp, w-4) {
				prefix := "    "
				if i == 0 {
					prefix = "  - "
				}
				lines = append(lines, prefix+l)
			}
		}
	}
	if strings.TrimSpace(written.Schedule.Task) != "" {
		lines = append(lines, "", "SCHEDULE TASK:")
		for _, l := range style.Wrap(written.Schedule.Task, w-2) {
			lines = append(lines, "  "+l)
		}
	}
	if len(written.Backend.Args) > 0 {
		lines = append(lines, "", "backend args as written: "+style.Clean(strings.Join(written.Backend.Args, " ")),
			"backend args it keeps:   "+style.Clean(strings.Join(eff.Backend.Args, " ")))
	}
	lines = append(lines, "")
	lines = append(lines, capabilityLines(dots.Requested(written), clamps, is.keep)...)
	if len(p.Memory) > 0 {
		lines = append(lines, "")
		switch {
		case is.memNote != "":
			lines = append(lines, fmt.Sprintf("MEMORY: %d entries, not imported (%s)", len(p.Memory), is.memNote))
		case !is.withMem:
			lines = append(lines, fmt.Sprintf("MEMORY: %d entries, not imported (m to include)", len(p.Memory)))
		default:
			lines = append(lines, fmt.Sprintf("MEMORY: %d entries into %s; they arrive as proposals for you to approve (m to leave out):", len(p.Memory), style.Clean(is.project.Name)))
		}
		for i, e := range p.Memory {
			text := fmt.Sprintf("[%s %s] %s", e.Scope, e.Kind, e.Text)
			if len(e.Paths) > 0 {
				text += " (paths: " + strings.Join(e.Paths, ", ") + ")"
			}
			for j, l := range style.Wrap(text, w-6) {
				prefix := "     "
				if j == 0 {
					prefix = fmt.Sprintf("  %2d.", i+1)
				}
				lines = append(lines, prefix+" "+l)
			}
		}
	}
	return m.fitLines(lines)
}

func (m Model) keysImportSource(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.scr, m.status = scrLibrary, ""
		return m, nil
	case "enter":
		src := strings.TrimSpace(m.input.Value())
		if src == "" {
			return m, nil
		}
		m.status = "reading " + style.Clean(src) + style.Sym("…")
		return m, m.loadPack(src)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) keysImportPreview(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	is := m.imp
	lines := m.importLines()
	if is.pg.key(k.String(), len(lines), m.bodyHeight(), m.measured()) {
		return m, nil
	}
	requested := dots.Requested(is.pack.Dot)
	switch s := k.String(); s {
	case "esc", "q":
		m.scr, m.imp, m.status = scrLibrary, nil, ""
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i, _ := strconv.Atoi(s); i <= len(requested) {
			is.keep[requested[i-1]] = !is.keep[requested[i-1]]
		}
	case "m":
		if is.memNote == "" && len(is.pack.Memory) > 0 {
			is.withMem = !is.withMem
		}
	case "n":
		m.scr, m.input = scrImportRename, newPrompt("new name", is.name)
		return m, textinput.Blink
	case "y":
		switch {
		case !m.measured():
			m.status = waitingForSize
			return m, nil
		case !is.pg.seenEnd:
			m.status = "scroll to the end of the preview first (j or space)"
			return m, nil
		case is.taken:
			m.status = "that name is taken: press n to choose another"
			return m, nil
		}
		if _, _, err := is.prepared(); err != nil {
			m.status = style.Clean(err.Error())
			return m, nil
		}
		return m, m.doImport()
	}
	return m, nil
}

func (m Model) keysImportRename(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.scr = scrImportPreview
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			return m, nil
		}
		m.imp.name = name
		m.imp.taken = dotTaken(m.d.DotsDir, name)
		m.imp.pg = pager{} // a new name: read the preview again
		m.imp.pg.fit(len(m.importLines()), m.bodyHeight(), m.measured())
		m.scr, m.status = scrImportPreview, ""
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// doImport saves the clamped dot (never over an existing one) and imports
// its memory as someone else's conclusions: proposals, through ImportPack.
func (m Model) doImport() tea.Cmd {
	is, dir, st := *m.imp, m.d.DotsDir, m.d.Memory
	keep := kept(is.keep)
	return func() tea.Msg {
		p := is.pack
		p.Dot.Name = is.name
		d, _, err := dotpack.Prepare(p, keep)
		if err != nil {
			return errMsg{err}
		}
		if err := dots.Save(dir, d, true); err != nil {
			if errors.Is(err, dots.ErrExists) {
				return errMsg{fmt.Errorf("a dot named %q already exists: choose another name", d.Name)}
			}
			return errMsg{err}
		}
		msg := importedMsg{name: d.Name, slug: d.Slug}
		if is.withMem && st != nil && is.project.ID != "" {
			msg.res, msg.memErr = st.ImportPack(is.project.ID, d.Slug, p.Memory)
		}
		return msg
	}
}

func (m Model) importView() string {
	var b strings.Builder
	sep := " " + style.Sym("·") + " "
	switch m.scr {
	case scrImportSource:
		b.WriteString(style.Title.Render("IMPORT A DOT PACK") + "\n\nA .dotpack.toml file path or an https:// URL:\n" + m.input.View() + "\n")
		if m.status != "" {
			b.WriteString(m.cutLine(style.Clean(m.status)) + "\n")
		}
		b.WriteString("\n" + style.Dim.Render("enter read"+sep+"esc back"))
	case scrImportRename:
		b.WriteString(style.Title.Render("SAVE AS") + "\n\n" + m.input.View() + "\n\n" + style.Dim.Render("enter use"+sep+"esc back"))
	default:
		b.WriteString(strings.Join(m.imp.pg.window(m.importLines(), m.bodyHeight()), "\n") + "\n")
		if m.status != "" {
			b.WriteString(m.cutLine(style.Clean(m.status)) + "\n")
		}
		mem := ""
		if m.imp.withMem {
			mem = sep + "memory: " + strconv.Itoa(len(m.imp.pack.Memory)) + " proposals"
		}
		b.WriteString(m.cutLine(grantsLine(m.imp.keep)+mem) + "\n")
		help := strings.Join([]string{"j/k scroll", "1-9 keep a capability", "m memory", "n rename", "y import", "esc cancel"}, sep)
		b.WriteString(style.Dim.Render(m.cutLine(help)))
	}
	return b.String()
}

// ---- export

type exportState struct {
	slug, name  string
	with        string // "", dotpack.WithDot, dotpack.WithDotProject
	includeUser bool
	dropSecrets bool     // leave out memory entries that look like secrets
	secrets     []string // what looked like a secret on the last try (field names, never values)
}

type exportedMsg struct {
	path             string
	entries, dropped int
}

type exportSecretMsg struct{ where []string }

func (m Model) startExport(slug string) (Model, tea.Cmd) {
	name := slug
	for _, d := range m.library() {
		if d.Slug == slug {
			name = d.Name
		}
	}
	dir := m.d.ExportDir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	m.exp = &exportState{slug: slug, name: name}
	m.input = newPrompt("file", shortHome(filepath.Join(dir, slug+dotpack.Ext))) // expandHome undoes it on enter
	m.scr, m.status = scrExport, ""
	return m, textinput.Blink
}

// onlyMemory reports whether every place a secret was found is a memory
// entry (which a forced export leaves out), and how many.
func onlyMemory(where []string) (bool, int) {
	n := 0
	for _, w := range where {
		if !strings.HasPrefix(w, "memory ") {
			return false, n
		}
		n++
	}
	return true, n
}

// writeNew writes data (0600) to a file that must not exist yet. It is
// crash-atomic and never replaces a file, even one created after the user
// pressed enter: the data goes to a temporary file in the same directory,
// which is then hard-linked to path (a link fails if path exists) and
// removed. The target appears whole or not at all.
func writeNew(path string, data []byte) error {
	exists := func() error { return fmt.Errorf("%s exists: choose another file name", shortHome(path)) }
	if _, err := os.Lstat(path); err == nil {
		return exists()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*") // 0600
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return exists()
		}
		return err
	}
	return nil
}

func (m Model) doExport() tea.Cmd {
	ex, dir, st, target, resolve := *m.exp, m.d.DotsDir, m.d.Memory, m.target, m.d.Project
	path := expandHome(strings.TrimSpace(m.input.Value()))
	pv, _ := m.snap.Pane(m.target)
	return func() tea.Msg {
		d, err := dots.Load(dir, ex.slug)
		if err != nil {
			return errMsg{err}
		}
		var mem []memory.Entry
		if ex.with != "" {
			if st == nil {
				return errMsg{errors.New("memory is turned off (memory_enabled = false)")}
			}
			pr := memory.Project{ID: pv.ProjectID}
			if pr.ID == "" && target != "" && resolve != nil {
				pr, _ = resolve(target)
			}
			if mem, err = dotpack.SelectMemory(st, pr.ID, ex.slug, ex.with, ex.includeUser); err != nil {
				return errMsg{err}
			}
		}
		opts := dotpack.ExportOptions{Version: version.Version}
		data, err := dotpack.Export(d, mem, opts)
		dropped := 0
		var se *dotpack.SecretError
		if errors.As(err, &se) {
			ok, n := onlyMemory(se.Where)
			if !ex.dropSecrets || !ok {
				return exportSecretMsg{where: se.Where}
			}
			// Force leaves the flagged memory entries out; the dot, the note
			// and the version are refused anyway.
			opts.Force, dropped = true, n
			data, err = dotpack.Export(d, mem, opts)
			if errors.As(err, &se) {
				return exportSecretMsg{where: se.Where}
			}
		}
		if err != nil {
			return errMsg{err}
		}
		if err := writeNew(path, data); err != nil {
			return errMsg{err}
		}
		return exportedMsg{path: path, entries: len(mem) - dropped, dropped: dropped}
	}
}

func (m Model) keysExport(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	ex := m.exp
	switch k.String() {
	case "esc":
		m.scr, m.exp, m.status = scrProfile, nil, ""
		if m.prof == nil {
			m.scr = scrLibrary
		}
		return m, nil
	case "tab":
		switch ex.with {
		case "":
			ex.with = dotpack.WithDot
		case dotpack.WithDot:
			ex.with = dotpack.WithDotProject
		default:
			ex.with = ""
		}
		ex.secrets, ex.dropSecrets = nil, false
		return m, nil
	case "ctrl+u":
		ex.includeUser = !ex.includeUser
		ex.secrets, ex.dropSecrets = nil, false
		return m, nil
	case "ctrl+e":
		if len(ex.secrets) == 0 {
			return m, nil
		}
		ex.dropSecrets = true
		m.status = ""
		return m, m.doExport()
	case "enter":
		m.status = ""
		return m, m.doExport()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) exportView() string {
	ex := m.exp
	with := "none"
	if ex.with != "" {
		with = ex.with
	}
	user := "no"
	if ex.includeUser {
		user = "yes"
	}
	var b strings.Builder
	b.WriteString(style.Title.Render("EXPORT "+style.Clean(ex.name)) + "\n\n")
	b.WriteString(fmt.Sprintf("memory:               %s   (tab: none / dot / dot+project)\n", with))
	b.WriteString(fmt.Sprintf("include your entries: %s   (ctrl+u)\n", user))
	b.WriteString("never included: credentials, history, global memory, preferences\n\nfile: " + m.input.View() + "\n")
	if len(ex.secrets) > 0 {
		names := make([]string, len(ex.secrets))
		for i, w := range ex.secrets {
			names[i] = style.Clean(w)
		}
		b.WriteString(strings.Join(m.fitLines([]string{style.Err.Render("looks like a secret: " + strings.Join(names, ", "))}), "\n") + "\n")
		if ok, _ := onlyMemory(ex.secrets); ok {
			b.WriteString("ctrl+e exports without those memory entries\n")
		} else {
			b.WriteString("a secret in the dot itself must be removed first\n")
		}
	}
	if m.status != "" {
		b.WriteString(strings.Join(m.fitLines([]string{style.Clean(m.status)}), "\n") + "\n")
	}
	sep := " " + style.Sym("·") + " "
	b.WriteString("\n" + style.Dim.Render("enter export"+sep+"esc back"))
	return b.String()
}
