package pane

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// patchHelp is appended to the help text.
const patchHelp = `w draft a fix (write-capable dots)   v view patch   A apply patch`

// PatchViewer opens the diff viewer on a patch card. apply opens it with the
// apply confirm armed: gate 2 always happens with the diff on screen.
type PatchViewer func(target, patchID, cardID string, apply bool) error

// WithPatchViewer sets how the pane opens the diff viewer (keys v and A).
func (m Model) WithPatchViewer(v PatchViewer) Model {
	m.viewer = v
	return m
}

type tickMsg time.Time

// draftForm is the gate-1 confirm. It is bound to the dots and the card
// captured when it opened, never to whatever is selected later, and it
// shows the whole instruction the dot will get.
type draftForm struct {
	choices []string // write-capable dots attached to the pane, by slug
	idx     int
	cardID  string // the card being fixed; "" when opened from the roster
	text    string // the instruction, exactly as it will be sent
	editing bool
	editor  textarea.Model
}

func (m Model) clockNow() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

func (m Model) libraryDot(slug string) (view.LibraryDot, bool) {
	for _, d := range m.snap.Library {
		if d.Slug == slug {
			return d, true
		}
	}
	return view.LibraryDot{}, false
}

// writeDots lists the pane's attached dots that may draft fixes.
func (m Model) writeDots() []string {
	var out []string
	for _, d := range m.pane().Dots {
		if ld, ok := m.libraryDot(d.Slug); ok && ld.Write {
			out = append(out, d.Slug)
		}
	}
	return out
}

func newDraftEditor() textarea.Model {
	ta := textarea.New()
	ta.SetHeight(4)
	ta.ShowLineNumbers = false
	return ta
}

// openDraft opens the confirm for the selected card (queue) or dot (roster).
func (m Model) openDraft() (Model, tea.Cmd) {
	if !m.snap.WriteEnabled {
		m.status = "write-capable dots are off: set write_enabled = true in config.toml"
		return m, nil
	}
	choices := m.writeDots()
	if len(choices) == 0 {
		m.status = "no write-capable dot here: set write = true under [capabilities] in a dot file"
		return m, nil
	}
	f := &draftForm{choices: choices, editor: newDraftEditor()}
	prefer := ""
	if m.area == areaQueue {
		c, ok := m.selectedCard()
		if !ok {
			m.status = "select a card to fix, or tab to the roster"
			return m, nil
		}
		if c.Kind == queue.KindPatch {
			m.status = "this card is already a patch"
			return m, nil
		}
		f.cardID, f.text, prefer = c.ID, strings.TrimSpace(textutil.SanitizeModelText(c.Body)), c.Dot
	} else {
		d, ok := m.selectedDot()
		if !ok {
			return m, nil
		}
		if !slices.Contains(choices, d.Slug) {
			m.status = d.Name + " can't draft fixes: set write = true under [capabilities] in its dot file"
			return m, nil
		}
		prefer = d.Slug
	}
	if i := slices.Index(choices, prefer); i >= 0 {
		f.idx = i
	}
	m.draft = f
	if f.text == "" {
		return m.editDraft()
	}
	return m, nil
}

func (m Model) editDraft() (Model, tea.Cmd) {
	f := m.draft
	f.editing = true
	f.editor.SetWidth(max(20, m.width-2))
	f.editor.SetValue(f.text)
	f.editor.CursorEnd()
	f.editor.Focus()
	return m, textarea.Blink
}

// updateDraft handles the confirm: y drafts, e edits the instruction, tab
// picks another write-capable dot, esc cancels.
func (m Model) updateDraft(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.draft
	if f.editing {
		switch k.String() {
		case "esc":
			f.editing = false
			return m, nil
		case "ctrl+s":
			f.text = strings.TrimSpace(textutil.SanitizeModelText(f.editor.Value()))
			f.editing = false
			return m, nil
		}
		var cmd tea.Cmd
		f.editor, cmd = f.editor.Update(k)
		return m, cmd
	}
	switch k.String() {
	case "esc", "n", "q":
		m.draft = nil
	case "tab":
		f.idx = (f.idx + 1) % len(f.choices)
	case "e":
		return m.editDraft()
	case "y":
		if !m.connected {
			m.status = offlineHint
			return m, nil
		}
		if f.text == "" {
			m.status = "say what to fix first: press e"
			return m, nil
		}
		slug := f.choices[f.idx]
		m.draft = nil
		m.status = "drafting with " + slug + "…"
		return m, m.call(ipc.CmdDraft, ipc.DraftArgs{PaneID: m.target, Slug: slug, Instruction: f.text, FromCardID: f.cardID})
	}
	return m, nil
}

// draftTools names the tools a draft run of this backend gets.
func draftTools(kind string) string {
	if kind == "claude-cli" {
		return "Read, Grep, Glob, Edit, Write; no shell, no web"
	}
	return "read_file, grep, list_files, write_file, edit_file; no shell"
}

func timeoutText(s int) string {
	if s > 0 && s%60 == 0 {
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%ds", s)
}

// draftView is the confirm, rendered in the footer so it never scrolls away.
func (m Model) draftView(w int) string {
	f := m.draft
	d, _ := m.libraryDot(f.choices[f.idx])
	body := lipgloss.NewStyle().Width(max(10, w-2))
	var b strings.Builder
	b.WriteString(rule("DRAFT A FIX", w))
	titleText := fmt.Sprintf("Draft a fix with %s?", textutil.SanitizeModelText(d.Name))
	if len(f.choices) > 1 {
		titleText += "  tab: another dot"
	}
	b.WriteString(style.Title.Render(trunc(titleText, max(10, w))) + "\n")
	model := d.Model
	if model == "" {
		model = "default model"
	}
	b.WriteString(trunc(fmt.Sprintf("%s · %s · timeout %s", d.Backend, model, timeoutText(d.WriteTimeoutSeconds)), max(10, w)) + "\n")
	b.WriteString(body.Render("tools: "+draftTools(d.Backend)) + "\n")
	b.WriteString(body.Render("Edits happen in an isolated copy; nothing changes in your files until you apply.") + "\n")
	if f.editing {
		b.WriteString(f.editor.View() + "\n" + style.Dim.Render(trunc("ctrl+s keep · esc cancel", max(10, w))) + "\n")
		return b.String()
	}
	b.WriteString("Instruction:\n")
	if f.text == "" {
		b.WriteString(style.Dim.Render("  (none yet: press e)") + "\n")
	} else {
		b.WriteString(body.Render("  "+f.text) + "\n")
	}
	b.WriteString(style.Dim.Render(trunc("y draft · e edit · esc cancel", max(10, w))) + "\n")
	return b.String()
}

// patchKeys handles w everywhere and v, A, x, e, M on a selected patch card.
// ok=false means "not a patch key": the normal handling runs (⏎ sends a
// patch card like any card; the engine sends where the patch is, not the diff).
func (m Model) patchKeys(k tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch k.String() {
	case "w":
		if !m.connected {
			m.status = offlineHint
			return m, nil, true
		}
		mm, cmd := m.openDraft()
		return mm, cmd, true
	case "v", "A", "x", "e", "M":
	default:
		return m, nil, false
	}
	c, ok := m.selectedCard()
	if !ok || c.Kind != queue.KindPatch || m.area != areaQueue {
		return m, nil, false
	}
	if !m.connected {
		m.status = offlineHint
		return m, nil, true
	}
	switch k.String() {
	case "v", "A":
		apply := k.String() == "A"
		if apply && c.Status != queue.StatusPending {
			m.status = "only a pending patch can be applied"
			return m, nil, true
		}
		if m.viewer == nil {
			m.status = "open the patch with the Dots: patch pane"
			return m, nil, true
		}
		open, target, id, card := m.viewer, m.target, c.PatchID, c.ID
		m.status = "opening the patch…"
		if apply {
			m.status = "review the diff, then y to apply"
		}
		return m, func() tea.Msg {
			if err := open(target, id, card, apply); err != nil {
				return errMsg{err}
			}
			return nil
		}, true
	case "x":
		return m, m.call(ipc.CmdDiscardPatch, ipc.CardArgs{ID: c.ID}), true
	case "e":
		m.status = "a patch card can't be edited: discard it (x) and draft again (w)"
	case "M":
		m.status = "a patch is remembered when you apply or discard it"
	}
	return m, nil, true
}

// tickIfDrafting keeps a once-a-second tick going while drafts run, so the
// elapsed time moves.
func (m Model) tickIfDrafting() (Model, tea.Cmd) {
	if m.ticking || len(m.pane().Drafts) == 0 {
		return m, nil
	}
	m.ticking = true
	return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// draftProgress is one line per running draft: "✎ Security drafting… 2:14".
func draftProgress(p view.Pane, now time.Time, w int) string {
	var b strings.Builder
	for _, d := range p.Drafts {
		name := d.Slug
		for _, ds := range p.Dots {
			if ds.Slug == d.Slug {
				name = ds.Name
			}
		}
		what := "drafting…"
		switch d.Phase {
		case view.DraftSnapshot:
			what = "snapshotting…"
		case view.DraftValidating:
			what = "checking the patch…"
		}
		el := max(0, int(now.Sub(d.StartedAt).Seconds()))
		b.WriteString(style.Dim.Render(trunc(fmt.Sprintf("%s %s %s %d:%02d", style.Sym("✎"), textutil.SanitizeModelText(name), what, el/60, el%60), max(10, w))) + "\n")
	}
	return b.String()
}

// cardLabel is a card header's label: its severity, or PATCH.
func cardLabel(c queue.Card) string {
	if c.Kind == queue.KindPatch {
		return "PATCH"
	}
	return strings.ToUpper(c.Severity)
}

// patchTags is the card header suffix for a patch card: its diffstat.
func patchTags(c queue.Card) string {
	if c.Kind != queue.KindPatch {
		return ""
	}
	return " · " + textutil.SanitizeModelText(c.DiffStat)
}

// patchDetail shows a patch card's flags and note.
func patchDetail(c queue.Card, w int) string {
	if c.Kind != queue.KindPatch {
		return ""
	}
	var b strings.Builder
	if len(c.PatchFlags) > 0 {
		b.WriteString(style.Dim.Render(trunc("  ["+textutil.SanitizeModelText(strings.Join(c.PatchFlags, "] ["))+"]", max(10, w))) + "\n")
	}
	if c.Note != "" {
		b.WriteString(style.Dim.Render(trunc("  "+strings.ReplaceAll(textutil.SanitizeModelText(c.Note), "\n", " "), max(10, w))) + "\n")
	}
	return b.String()
}

// patchHint is the selected patch card's key line.
const patchHint = "  v view  A apply  ⏎ send to agent  x discard"

// patchText is a patch card's summary, sanitized: it is model text.
func patchText(c queue.Card) string {
	if c.Kind != queue.KindPatch {
		return c.Body
	}
	return textutil.SanitizeModelText(c.Body)
}
