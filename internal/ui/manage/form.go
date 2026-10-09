package manage

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

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
}

// Form edits every setting of one dot. orig keeps fields the form doesn't
// show (backend args), so saving never drops them.
type Form struct {
	orig   dots.Dot
	isNew  bool
	fields []field
	focus  int
	err    string
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
		choiceField("Color", dots.Colors, d.Color),
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
	}}
	f.setFocus(0)
	return f
}

func (f *Form) IsNew() bool       { return f.isNew }
func (f *Form) SetError(s string) { f.err = s }

func (f *Form) setFocus(i int) {
	n := len(f.fields)
	f.focus = ((i % n) + n) % n
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
		switch k.String() {
		case "right", "l", " ":
			cur.idx = (cur.idx + 1) % len(cur.choices)
		case "left", "h":
			cur.idx = (cur.idx - 1 + len(cur.choices)) % len(cur.choices)
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
	for i, fl := range f.fields {
		label := fmt.Sprintf("%-24s", fl.label)
		if i == f.focus {
			label = style.Selected.Render(label)
		}
		var v string
		switch fl.kind {
		case kChoice:
			v = "‹ " + fl.choices[fl.idx] + " ›"
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
	}
	if f.err != "" {
		b.WriteString("\n" + style.Err.Render(f.err) + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("tab/shift+tab move · ←/→ change · space toggle · ctrl+s save · esc cancel"))
	_ = width
	return b.String()
}
