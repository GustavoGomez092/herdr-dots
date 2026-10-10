package manage

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ui/style"
)

type fieldKind int

const (
	kText fieldKind = iota
	kArea
	kChoice
	kBool
	kInt
	kList
)

type field struct {
	label   string
	kind    fieldKind
	input   textinput.Model
	area    textarea.Model
	choices []string
	idx     int
	on      bool
	kinds   []string // backend kinds this field applies to; nil = all
}

// Form edits every setting of one dot. orig keeps fields the form doesn't
// show (backend args), so saving never drops them.
type Form struct {
	orig      dots.Dot
	isNew     bool
	fields    []field
	focus     int
	err       string
	estimate  func(dots.Dot) string // editor cost line; nil = none
	models    []string              // ctrl+space choices for the current kind
	modelsOf  string                // kind the choices belong to
	modelIdx  int
	now       func() time.Time  // the cron preview's clock; nil = time.Now
	height    int               // screen height for scrolling (SetHeight); 0 = unknown
	problems  map[string]string // field label -> validation message, set by View
	unseen    bool              // the focused field's line was not drawn by the last View
	lastWidth int               // the last real width View rendered at (0 = none yet)
}

func textField(label, v string) field {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.SetValue(v)
	return field{label: label, kind: kText, input: ti}
}

func intField(label string, v int) field {
	f := textField(label, strconv.Itoa(v))
	f.kind = kInt
	return f
}

func listField(label string, v []string) field {
	f := textField(label, strings.Join(v, ", "))
	f.kind = kList
	return f
}

func choiceField(label string, choices []string, v string) field {
	return field{label: label, kind: kChoice, choices: choices, idx: max(0, slices.Index(choices, v))}
}

// colorChoices are the palette, plus a color the dot file names that the
// palette lacks, so an edit never silently rewrites it (U11).
func colorChoices(cur string) []string {
	if cur == "" || slices.Contains(dots.Colors, cur) {
		return dots.Colors
	}
	return append(slices.Clone(dots.Colors), cur)
}

func NewForm(d dots.Dot, isNew bool) *Form {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetHeight(6)
	ta.SetWidth(64)
	ta.CharLimit = 4000
	ta.SetValue(d.Purpose)
	modes := make([]string, len(dots.Modes))
	for i, m := range dots.Modes {
		modes[i] = string(m)
	}
	f := &Form{orig: d, isNew: isNew, fields: []field{
		textField("Name", d.Name),
		textField("Glyph", d.Glyph),
		choiceField("Color", colorChoices(d.Color), d.Color),
		{label: "Purpose", kind: kArea, area: ta},
		choiceField("Backend", dots.BackendKinds, d.Backend.Kind),
		textField("Model", d.Backend.Model),
		choiceField("Mode", modes, string(d.Delivery.Mode)),
		{label: "Review every turn", kind: kBool, on: d.Trigger.OnTurn},
		listField("File globs", d.Trigger.FileGlobs),
		intField("Cooldown (turns)", d.Trigger.Cooldown),
		intField("Context turns", d.Context.Turns),
		{label: "Read repo (read-only)", kind: kBool, on: d.Context.RepoRead},
		choiceField("Verbosity", dots.Verbosities, d.Context.Verbosity),
		intField("Budget (calls/session)", d.Budget.MaxCallsPerSession),
		// Phase 4: shown only for the kinds they apply to (field.kinds).
		withKinds(choiceField("Effort", effortChoices, effortLabel(d.Backend.Effort)), dots.KindAnthropicAPI, dots.KindOpenAIAPI),
		withKinds(intField("Max tokens (0 = 4096)", d.Backend.MaxTokens), dots.KindAnthropicAPI, dots.KindOpenAIAPI, dots.KindOpenRouter, dots.KindOllama),
		withKinds(textField("Base URL", d.Backend.BaseURL), dots.KindOllama, dots.KindOpenRouter),
		withKinds(intField("Context window (0 = 16384)", d.Backend.ContextWindow), dots.KindOllama),
		withKinds(choiceField("Refusal fallbacks", dots.FallbackModes, fallbackLabel(d.Backend.Fallbacks)), dots.KindAnthropicAPI),
		textField("Budget ($/session, 0 = off)", strconv.FormatFloat(d.Budget.MaxUSDPerSession, 'f', -1, 64)),
	}}
	f.fields = append(f.fields, proactiveFields(d)...) // Phase 8 (form_proactive.go)
	f.setFocus(0)
	return f
}

// effortChoices shows "" as "default" (the provider's own default effort).
var effortChoices = append([]string{"default"}, dots.Efforts...)

func effortLabel(e string) string {
	if e == "" {
		return "default"
	}
	return e
}

func fallbackLabel(fb string) string {
	if fb == "" {
		return "default"
	}
	return fb
}

func withKinds(fl field, kinds ...string) field {
	fl.kinds = kinds
	return fl
}

// SetEstimator sets the function behind the editor's cost line.
func (f *Form) SetEstimator(fn func(dots.Dot) string) { f.estimate = fn }

// Kind is the backend kind currently selected.
func (f *Form) Kind() string { return f.fields[iBackend].choices[f.fields[iBackend].idx] }

// OnModel reports whether the Model field has focus (ctrl+space completes it).
func (f *Form) OnModel() bool { return f.focus == iModel }

// BaseURL is the Base URL field when the kind shows it, else "".
func (f *Form) BaseURL() string {
	if !f.visible(iBaseURL) {
		return ""
	}
	return f.val(iBaseURL)
}

// SetModels loads ctrl+space choices for kind and fills in the first one.
func (f *Form) SetModels(kind string, ids []string) {
	if kind != f.Kind() || len(ids) == 0 {
		return
	}
	f.models, f.modelsOf, f.modelIdx = ids, kind, 0
	f.fields[iModel].input.SetValue(ids[0])
	f.fields[iModel].input.CursorEnd()
}

// NextModel cycles the loaded choices; false means none are loaded for the
// current kind (the caller fetches them).
func (f *Form) NextModel() bool {
	if f.modelsOf != f.Kind() || len(f.models) == 0 {
		return false
	}
	f.modelIdx = (f.modelIdx + 1) % len(f.models)
	f.fields[iModel].input.SetValue(f.models[f.modelIdx])
	f.fields[iModel].input.CursorEnd()
	return true
}

func (f *Form) visible(i int) bool {
	ks := f.fields[i].kinds
	return ks == nil || slices.Contains(ks, f.Kind())
}

func (f *Form) IsNew() bool       { return f.isNew }
func (f *Form) SetError(s string) { f.err = s }

func (f *Form) setFocus(i int) {
	n := len(f.fields)
	step := 1
	if i < f.focus {
		step = -1
	}
	i = ((i % n) + n) % n
	for tries := 0; tries < n && !f.visible(i); tries++ {
		i = ((i+step)%n + n) % n // skip fields hidden for this kind
	}
	f.focus = i
	for j := range f.fields {
		fl := &f.fields[j]
		fl.input.Blur()
		fl.area.Blur()
		if j != f.focus {
			continue
		}
		switch fl.kind {
		case kText, kInt, kList:
			fl.input.Focus()
			fl.input.CursorEnd()
		case kArea:
			fl.area.Focus()
		}
	}
}

func (f *Form) Update(k tea.KeyMsg) tea.Cmd {
	cur := &f.fields[f.focus]
	nav := false
	switch k.String() {
	case "tab", "shift+tab":
		nav = true
	case "up", "down":
		nav = cur.kind != kArea
	}
	if !nav {
		// Never act on an unseen selection: lay the form out as it will be
		// drawn, and ignore edit keys while the focused line is off screen.
		if f.height > 0 {
			if f.lastWidth <= 0 {
				return nil // never rendered at a real width: the line may be off screen
			}
			f.View(f.lastWidth)
		}
		if f.unseen {
			return nil
		}
	}
	switch k.String() {
	case "tab":
		f.setFocus(f.focus + 1)
		return nil
	case "shift+tab":
		f.setFocus(f.focus - 1)
		return nil
	case "up":
		if cur.kind != kArea {
			f.setFocus(f.focus - 1)
			return nil
		}
	case "down":
		if cur.kind != kArea {
			f.setFocus(f.focus + 1)
			return nil
		}
	}
	var cmd tea.Cmd
	switch cur.kind {
	case kChoice:
		prevKind := f.Kind()
		switch k.String() {
		case "right", "l", " ":
			cur.idx = (cur.idx + 1) % len(cur.choices)
		case "left", "h":
			cur.idx = (cur.idx - 1 + len(cur.choices)) % len(cur.choices)
		}
		if f.focus == iBackend && f.Kind() != prevKind {
			// A base URL belongs to one kind: never carry it to another host
			// (an OpenRouter key must not be sent to an Ollama address).
			if len(f.fields[iBaseURL].kinds) > 0 && slices.Contains(f.fields[iBaseURL].kinds, prevKind) {
				f.fields[iBaseURL].input.SetValue("")
			}
			// Swap the model only if it was the old kind's default.
			if m := &f.fields[iModel].input; strings.TrimSpace(m.Value()) == dots.DefaultModelFor(prevKind) {
				m.SetValue(dots.DefaultModelFor(f.Kind()))
			}
		}
	case kBool:
		switch k.String() {
		case " ", "enter", "left", "right":
			cur.on = !cur.on
		}
	case kArea:
		cur.area, cmd = cur.area.Update(k)
	default:
		cur.input, cmd = cur.input.Update(k)
	}
	return cmd
}

func (f *Form) val(i int) string { return strings.TrimSpace(f.fields[i].input.Value()) }

func (f *Form) num(i int) (int, error) {
	n, err := strconv.Atoi(f.val(i))
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", f.fields[i].label)
	}
	return n, nil
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// field indexes, in NewForm order
const (
	iName = iota
	iGlyph
	iColor
	iPurpose
	iBackend
	iModel
	iMode
	iOnTurn
	iGlobs
	iCooldown
	iTurns
	iRepoRead
	iVerbosity
	iBudget
	iEffort
	iMaxTokens
	iBaseURL
	iContextWindow
	iFallbacks
	iBudgetUSD
)

func (f *Form) Dot() (dots.Dot, error) {
	d := f.orig
	fl := f.fields
	d.Name = f.val(iName)
	d.Glyph = f.val(iGlyph)
	d.Color = fl[iColor].choices[fl[iColor].idx]
	d.Purpose = strings.TrimSpace(fl[iPurpose].area.Value())
	d.Backend.Kind = fl[iBackend].choices[fl[iBackend].idx]
	d.Backend.Model = f.val(iModel)
	d.Delivery.Mode = dots.Mode(fl[iMode].choices[fl[iMode].idx])
	d.Trigger.OnTurn = fl[iOnTurn].on
	d.Trigger.FileGlobs = splitList(f.fields[iGlobs].input.Value())
	d.Context.RepoRead = fl[iRepoRead].on
	d.Context.Verbosity = fl[iVerbosity].choices[fl[iVerbosity].idx]
	var err error
	if d.Trigger.Cooldown, err = f.num(iCooldown); err != nil {
		return d, err
	}
	if d.Context.Turns, err = f.num(iTurns); err != nil {
		return d, err
	}
	if d.Budget.MaxCallsPerSession, err = f.num(iBudget); err != nil {
		return d, err
	}
	// Hidden fields are written as zero values, so switching kinds never
	// leaves a setting the new kind rejects.
	d.Backend.Effort, d.Backend.MaxTokens, d.Backend.BaseURL, d.Backend.ContextWindow, d.Backend.Fallbacks = "", 0, "", 0, ""
	if f.visible(iEffort) {
		if e := fl[iEffort].choices[fl[iEffort].idx]; e != "default" {
			d.Backend.Effort = e
		}
	}
	if f.visible(iMaxTokens) {
		if d.Backend.MaxTokens, err = f.num(iMaxTokens); err != nil {
			return d, err
		}
	}
	if f.visible(iBaseURL) {
		d.Backend.BaseURL = f.val(iBaseURL)
	}
	if f.visible(iContextWindow) {
		if d.Backend.ContextWindow, err = f.num(iContextWindow); err != nil {
			return d, err
		}
	}
	if f.visible(iFallbacks) {
		if fb := fl[iFallbacks].choices[fl[iFallbacks].idx]; fb != "default" || f.orig.Backend.Fallbacks == "default" {
			d.Backend.Fallbacks = fb
		}
	}
	if dots.IsAPIKind(d.Backend.Kind) {
		d.Backend.Args = []string{} // CLI flags mean nothing to an API backend
	}
	usd := f.val(iBudgetUSD)
	if usd == "" {
		usd = "0"
	}
	if d.Budget.MaxUSDPerSession, err = strconv.ParseFloat(usd, 64); err != nil {
		return d, fmt.Errorf("%s must be a number like 0.50", f.fields[iBudgetUSD].label)
	}
	if err := f.applyProactive(&d); err != nil {
		return d, err
	}
	if f.isNew {
		d.Slug = dots.Slugify(d.Name)
	}
	return d, nil
}

func (f *Form) View(width int) string {
	var b strings.Builder
	title := "EDIT DOT"
	if f.isNew {
		title = "NEW DOT"
	}
	b.WriteString(style.Title.Render(title) + "\n\n")
	if width > 0 {
		f.lastWidth = width
	}
	f.refreshProblems()
	fieldsStart, focusStart, focusEnd := strings.Count(b.String(), "\n"), -1, -1
	for i, fl := range f.fields {
		if !f.visible(i) {
			continue
		}
		if i == f.focus {
			focusStart = strings.Count(b.String(), "\n")
		}
		label := fmt.Sprintf("%-24s", fl.label)
		if i == f.focus {
			label = style.Selected.Render(label)
		}
		var v string
		switch fl.kind {
		case kChoice:
			v = "‹ " + style.Clean(fl.choices[fl.idx]) + " ›" // a kept file color is the file's text
		case kBool:
			v = "[ ]"
			if fl.on {
				v = "[x]"
			}
		case kArea:
			v = "\n" + fl.area.View()
		default:
			v = fl.input.View()
		}
		b.WriteString(label + " " + v + "\n")
		if n := f.proactiveNote(fl.label); n != "" {
			for _, line := range strings.Split(n, "\n") {
				if strings.HasPrefix(line, "  ! ") {
					b.WriteString(style.Err.Render(line) + "\n")
				} else {
					b.WriteString(style.Dim.Render(line) + "\n")
				}
			}
		}
		if i == f.focus {
			focusEnd = strings.Count(b.String(), "\n") - 1
		}
	}
	fieldsEnd := strings.Count(b.String(), "\n")
	if f.estimate != nil {
		if d, err := f.Dot(); err == nil {
			if line := f.estimate(d); line != "" {
				b.WriteString("\n" + style.Dim.Render(wrapTo(line, width)) + "\n")
			}
		}
	}
	if f.err != "" {
		b.WriteString("\n" + style.Err.Render(wrapTo(f.err, width)) + "\n")
	}
	help := "tab/shift+tab move · ←/→ change · space toggle · ctrl+s save · esc cancel"
	if f.OnModel() {
		help = "ctrl+space pick a model · " + help
	}
	b.WriteString("\n" + style.Dim.Render(wrapHelp(help, width)))
	return f.fit(clipLines(b.String(), width), fieldsStart, fieldsEnd, focusStart, focusEnd)
}
