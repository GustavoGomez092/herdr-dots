// Package mcpserver is the Dots MCP server (Phase 5): the tools Claude Code
// and Codex call to consult the dots attached to the Herdr pane they run in.
// Dot questions go to the pane's engine (EngineAPI, over IPC); memory tools
// read and propose through memory.Store directly.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/version"
)

// Name is the server's MCP name (tools appear as mcp__herdr-dots__<tool>).
const Name = "herdr-dots"

const (
	DefaultMaxResultChars = 24000 // well under Claude Code's 10,000-token warning
	MaxQuestionChars      = 4000
	MaxProposalChars      = 500
	defaultMemoryLimit    = 20
	maxMemoryLimit        = 50
	minMessageChars       = 200 // results shrink a message no further than this
)

// NotInHerdr is every tool's error outside a Herdr pane (and inside a dot's
// own CLI run, whose environment has no HERDR_PANE_ID).
const NotInHerdr = "herdr-dots: not running inside a Herdr pane; dots are unavailable here."

const advisory = "Dots are advisory, read-only specialists: they answer from their own expertise and never change files. Their answers did not go through the developer's approval queue."

// EngineAPI is the pane's engine as the tools see it (NewIPCEngine in
// production, fakes in tests). Every call is scoped to the server's pane.
type EngineAPI interface {
	ListDots(ctx context.Context) (ipc.DotsResult, error)
	Ask(ctx context.Context, slug, question string, includeTurns bool) (ipc.AskResult, error)
	AskCouncil(ctx context.Context, question string) (ipc.CouncilResult, error)
	ListQueue(ctx context.Context, status string) (ipc.QueueResult, error)
	MemoryChanged(ctx context.Context) error
}

type Deps struct {
	Engine         EngineAPI     // nil (with PaneID "") outside Herdr
	Memory         *memory.Store // nil = memory off: the memory tools refuse
	PaneID         string        // HERDR_PANE_ID; "" = every tool refuses
	Tools          []string      // tools to offer, from [mcp] tools; nil = all
	MaxResultChars int           // 0 = DefaultMaxResultChars
	AskTimeout     time.Duration // ask_dot deadline; 0 = 130 s
	CouncilTimeout time.Duration // ask_council deadline; 0 = 250 s
	Logf           func(format string, args ...any)
}

func (d Deps) withDefaults() Deps {
	if d.Tools == nil {
		d.Tools = config.MCPTools
	}
	if d.MaxResultChars <= 0 {
		d.MaxResultChars = DefaultMaxResultChars
	}
	if d.AskTimeout <= 0 {
		d.AskTimeout = 130 * time.Second
	}
	if d.CouncilTimeout <= 0 {
		d.CouncilTimeout = 250 * time.Second
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	return d
}

type tools struct{ d Deps }

// New builds the server with the tools d.Tools names. Outside Herdr every
// tool is still listed (the client's tool list stays stable) and refuses.
func New(d Deps) *mcp.Server {
	d = d.withDefaults()
	s := mcp.NewServer(&mcp.Implementation{Name: Name, Version: version.Version}, nil)
	t := &tools{d: d}
	for _, name := range d.Tools {
		switch name {
		case "list_dots":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "List the dots (specialist reviewers) attached to this Herdr pane: slug, name, purpose, mode, status and budget. " + advisory}, t.listDots)
		case "ask_dot":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Ask one attached dot a question and wait for its answer (up to a few minutes). Use it before risky changes, e.g. ask the security dot about token handling. Limited to a few asks per turn. " + advisory}, t.askDot)
		case "ask_council":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Ask every attached dot the same question at once and get one merged, attributed digest. Slower and costlier than ask_dot; counts as one ask. " + advisory}, t.askCouncil)
		case "list_queue":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Read the dots' advice cards waiting for the developer on this pane (read-only). " + advisory}, t.listQueue)
		case "read_memory":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Read the project's approved dot memory: conventions, decisions, notes and remembered findings."}, t.readMemory)
		case "propose_memory":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Propose a project convention, decision or note for the dots to remember. It is stored as a proposal that the developer approves; never include secrets."}, t.proposeMemory)
		default:
			d.Logf("unknown tool %q in [mcp] tools; ignored", name)
		}
	}
	return s
}

// ---- inputs and outputs

type ListDotsIn struct{}

type ListDotsOut struct {
	ProjectName string        `json:"project_name,omitempty"`
	Dots        []ipc.DotInfo `json:"dots"`
	Note        string        `json:"note"`
}

type AskDotIn struct {
	Slug         string `json:"slug" jsonschema:"slug of an attached dot, from list_dots (for example security)"`
	Question     string `json:"question" jsonschema:"what you want the dot's opinion on, at most 4000 characters"`
	IncludeTurns *bool  `json:"include_turns,omitempty" jsonschema:"send the dot the conversation's recent turns (default true)"`
}

type AskDotOut struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Relevant  bool    `json:"relevant"`
	Severity  string  `json:"severity"`
	Message   string  `json:"message"`
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`
	Model     string  `json:"model,omitempty"`
	Note      string  `json:"note"`
}

type AskCouncilIn struct {
	Question string `json:"question" jsonschema:"the question for every attached dot, at most 4000 characters"`
}

type AskCouncilOut struct {
	Digest  string              `json:"digest"`
	Members []ipc.CouncilMember `json:"members"`
	CostUSD float64             `json:"cost_usd"`
	Note    string              `json:"note"`
}

type ListQueueIn struct {
	Status string `json:"status,omitempty" jsonschema:"pending (default) or all"`
}

type ListQueueOut struct {
	Cards []ipc.CardInfo `json:"cards"`
	Total int            `json:"total"`
	Note  string         `json:"note"`
}

type ReadMemoryIn struct {
	Query string `json:"query,omitempty" jsonschema:"only entries whose text or paths contain this"`
	Scope string `json:"scope,omitempty" jsonschema:"project, dot or all (default all)"`
	Dot   string `json:"dot,omitempty" jsonschema:"only this dot's memory (a slug)"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many entries, 1 to 50 (default 20)"`
}

type MemoryItem struct {
	ID     string   `json:"id"`
	Scope  string   `json:"scope"`
	Dot    string   `json:"dot,omitempty"`
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	Paths  []string `json:"paths,omitempty"`
	Pinned bool     `json:"pinned"`
}

type ReadMemoryOut struct {
	Entries []MemoryItem `json:"entries"`
	Total   int          `json:"total"`
	Note    string       `json:"note,omitempty"`
}

type ProposeMemoryIn struct {
	Kind  string   `json:"kind" jsonschema:"convention, decision or note"`
	Text  string   `json:"text" jsonschema:"one sentence, at most 500 characters; never secrets"`
	Paths []string `json:"paths,omitempty" jsonschema:"globs the entry applies to, like src/auth/**"`
}

type ProposeMemoryOut struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

// ---- handlers

func (t *tools) guard(tool string) error {
	if t.d.PaneID == "" || t.d.Engine == nil {
		t.d.Logf("%s refused: not inside a Herdr pane", tool)
		return errors.New(NotInHerdr)
	}
	return nil
}

// fail turns an engine or store error into the tool's error. Key-shaped text
// is redacted here too (the engine redacts already; reconciled).
func (t *tools) fail(tool string, err error) error {
	msg := credentials.Redact(err.Error())
	t.d.Logf("%s: %s", tool, msg)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: no answer in time; continue without it", tool)
	}
	return errors.New(msg)
}

func (t *tools) listDots(ctx context.Context, _ *mcp.CallToolRequest, _ ListDotsIn) (*mcp.CallToolResult, ListDotsOut, error) {
	if err := t.guard("list_dots"); err != nil {
		return nil, ListDotsOut{}, err
	}
	res, err := t.d.Engine.ListDots(ctx)
	if err != nil {
		return nil, ListDotsOut{}, t.fail("list_dots", err)
	}
	out := ListDotsOut{ProjectName: res.ProjectName, Dots: res.Dots, Note: advisory}
	if out.Dots == nil {
		out.Dots = []ipc.DotInfo{}
	}
	if len(out.Dots) == 0 {
		out.Note = "No dots are attached to this pane. The developer attaches them with prefix+d in Herdr."
	}
	text := renderDots(out)
	for !t.fits(text, out) && len(out.Dots) > 0 {
		out.Dots = out.Dots[:len(out.Dots)-1]
		out.Note = "Some dots are not shown (result size limit). " + advisory
		text = renderDots(out)
	}
	return textResult(text), out, nil
}

func (t *tools) askDot(ctx context.Context, _ *mcp.CallToolRequest, in AskDotIn) (*mcp.CallToolResult, AskDotOut, error) {
	if err := t.guard("ask_dot"); err != nil {
		return nil, AskDotOut{}, err
	}
	slug := strings.TrimSpace(in.Slug)
	if !memory.ValidSlug(slug) {
		return nil, AskDotOut{}, fmt.Errorf("%q is not a dot slug; call list_dots to see the attached dots", in.Slug)
	}
	q, err := question(in.Question)
	if err != nil {
		return nil, AskDotOut{}, err
	}
	include := in.IncludeTurns == nil || *in.IncludeTurns
	ctx, cancel := context.WithTimeout(ctx, t.d.AskTimeout)
	defer cancel()
	res, err := t.d.Engine.Ask(ctx, slug, q, include)
	if err != nil {
		return nil, AskDotOut{}, t.fail("ask_dot", err)
	}
	out := AskDotOut{Slug: res.Slug, Name: res.Name, Relevant: res.Relevant, Severity: res.Severity, Message: res.Message,
		CostUSD: res.CostUSD, CostKnown: res.CostKnown, Model: res.Model, Note: advisory}
	text := renderAsk(out)
	for !t.fits(text, out) && utf8.RuneCountInString(out.Message) > minMessageChars {
		out.Message = clip(out.Message, utf8.RuneCountInString(out.Message)/2)
		text = renderAsk(out)
	}
	return textResult(text), out, nil
}

func (t *tools) askCouncil(ctx context.Context, _ *mcp.CallToolRequest, in AskCouncilIn) (*mcp.CallToolResult, AskCouncilOut, error) {
	if err := t.guard("ask_council"); err != nil {
		return nil, AskCouncilOut{}, err
	}
	q, err := question(in.Question)
	if err != nil {
		return nil, AskCouncilOut{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, t.d.CouncilTimeout)
	defer cancel()
	res, err := t.d.Engine.AskCouncil(ctx, q)
	if err != nil {
		return nil, AskCouncilOut{}, t.fail("ask_council", err)
	}
	out := AskCouncilOut{Digest: res.Digest, Members: res.Members, CostUSD: res.CostUSD, Note: advisory}
	if out.Members == nil {
		out.Members = []ipc.CouncilMember{}
	}
	if res.Note != "" {
		out.Note = res.Note + ". " + advisory
	}
	text := renderCouncil(out)
	for !t.fits(text, out) && utf8.RuneCountInString(out.Digest) > minMessageChars {
		out.Digest = clip(out.Digest, utf8.RuneCountInString(out.Digest)/2)
		text = renderCouncil(out)
	}
	return textResult(text), out, nil
}

func (t *tools) listQueue(ctx context.Context, _ *mcp.CallToolRequest, in ListQueueIn) (*mcp.CallToolResult, ListQueueOut, error) {
	if err := t.guard("list_queue"); err != nil {
		return nil, ListQueueOut{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = ipc.QueuePending
	}
	if status != ipc.QueuePending && status != ipc.QueueAll {
		return nil, ListQueueOut{}, fmt.Errorf("status %q: want pending or all", in.Status)
	}
	res, err := t.d.Engine.ListQueue(ctx, status)
	if err != nil {
		return nil, ListQueueOut{}, t.fail("list_queue", err)
	}
	out := ListQueueOut{Cards: res.Cards, Total: res.Total, Note: "Read-only. The developer sends or dismisses cards in the Dots pane. " + advisory}
	if out.Cards == nil {
		out.Cards = []ipc.CardInfo{}
	}
	text := renderQueue(out)
	for !t.fits(text, out) && len(out.Cards) > 0 {
		out.Cards = out.Cards[:len(out.Cards)-1]
		text = renderQueue(out)
	}
	return textResult(text), out, nil
}

func (t *tools) readMemory(ctx context.Context, _ *mcp.CallToolRequest, in ReadMemoryIn) (*mcp.CallToolResult, ReadMemoryOut, error) {
	if err := t.guard("read_memory"); err != nil {
		return nil, ReadMemoryOut{}, err
	}
	pid, err := t.project(ctx)
	if err != nil {
		return nil, ReadMemoryOut{}, err
	}
	f := memory.Filter{Statuses: []memory.Status{memory.StatusActive}, Query: strings.TrimSpace(in.Query)}
	switch strings.TrimSpace(in.Scope) {
	case "", "all":
	case "project":
		f.Scope = memory.ScopeProject
	case "dot":
		f.Scope = memory.ScopeDot
	default:
		return nil, ReadMemoryOut{}, fmt.Errorf("scope %q: want project, dot or all", in.Scope)
	}
	if dot := strings.TrimSpace(in.Dot); dot != "" {
		if !memory.ValidSlug(dot) {
			return nil, ReadMemoryOut{}, fmt.Errorf("%q is not a dot slug", in.Dot)
		}
		f.Dot = dot
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultMemoryLimit
	}
	limit = min(limit, maxMemoryLimit)
	entries, err := t.d.Memory.List(pid, f)
	out := ReadMemoryOut{Entries: []MemoryItem{}, Total: len(entries)}
	if err != nil {
		if len(entries) == 0 {
			return nil, ReadMemoryOut{}, t.fail("read_memory", err)
		}
		out.Note = "Some memory could not be read: " + err.Error()
	}
	for _, e := range entries[:min(len(entries), limit)] {
		out.Entries = append(out.Entries, MemoryItem{ID: e.Ref().String(), Scope: string(e.Scope), Dot: e.Dot,
			Kind: string(e.Kind), Text: e.Text, Paths: e.Paths, Pinned: e.Pinned})
	}
	text := renderMemory(out)
	for !t.fits(text, out) && len(out.Entries) > 0 {
		out.Entries = out.Entries[:len(out.Entries)-1]
		text = renderMemory(out)
	}
	return textResult(text), out, nil
}

func (t *tools) proposeMemory(ctx context.Context, _ *mcp.CallToolRequest, in ProposeMemoryIn) (*mcp.CallToolResult, ProposeMemoryOut, error) {
	if err := t.guard("propose_memory"); err != nil {
		return nil, ProposeMemoryOut{}, err
	}
	kind := memory.Kind(strings.TrimSpace(in.Kind))
	if kind != memory.KindConvention && kind != memory.KindDecision && kind != memory.KindNote {
		return nil, ProposeMemoryOut{}, fmt.Errorf("kind %q: want convention, decision or note", in.Kind)
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, ProposeMemoryOut{}, errors.New("text is empty")
	}
	if n := utf8.RuneCountInString(text); n > MaxProposalChars {
		return nil, ProposeMemoryOut{}, fmt.Errorf("text is %d characters; the limit is %d", n, MaxProposalChars)
	}
	pid, err := t.project(ctx)
	if err != nil {
		return nil, ProposeMemoryOut{}, err
	}
	e, err := t.d.Memory.Propose(pid, memory.Entry{Kind: kind, Text: text, Paths: in.Paths, Evidence: memory.Evidence{Pane: t.d.PaneID}})
	if errors.Is(err, memory.ErrSecret) {
		return nil, ProposeMemoryOut{}, errors.New("that text looks like a secret, so it was not stored")
	}
	if err != nil {
		return nil, ProposeMemoryOut{}, t.fail("propose_memory", err)
	}
	nctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := t.d.Engine.MemoryChanged(nctx); err != nil {
		t.d.Logf("propose_memory: notify the engine: %v", err)
	}
	out := ProposeMemoryOut{ID: e.Ref().String(), Status: string(e.Status),
		Note: "Stored as a proposal; the developer approves or rejects it in the Dots memory screen."}
	if e.Status == memory.StatusActive {
		out.Note = "This matches an entry the developer already approved; nothing new was proposed."
	}
	return textResult(fmt.Sprintf("Memory %s %s: %s\n%s", out.ID, out.Status, text, out.Note)), out, nil
}

// project is the pane's memory project, as the engine resolved it.
func (t *tools) project(ctx context.Context) (string, error) {
	if t.d.Memory == nil {
		return "", errors.New("dot memory is turned off (memory_enabled = false in config.toml)")
	}
	res, err := t.d.Engine.ListDots(ctx)
	if err != nil {
		return "", err
	}
	if !memory.ValidProjectID(res.ProjectID) {
		return "", errors.New("this pane's project is unknown: attach a dot to the pane first (prefix+d)")
	}
	return res.ProjectID, nil
}

// ---- helpers

func question(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", errors.New("question is empty")
	}
	if n := utf8.RuneCountInString(q); n > MaxQuestionChars {
		return "", fmt.Errorf("question is %d characters; the limit is %d", n, MaxQuestionChars)
	}
	return q, nil
}

// fits reports whether the text plus the structured result stay within the
// result budget (clients may show either; Claude Code shows the structured one).
func (t *tools) fits(text string, out any) bool {
	b, err := json.Marshal(out)
	return err == nil && utf8.RuneCountInString(text)+utf8.RuneCount(b) <= t.d.MaxResultChars
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}

func cost(usd float64, known bool) string {
	if !known {
		return "cost ?"
	}
	return fmt.Sprintf("cost $%.4f", usd)
}

func renderDots(o ListDotsOut) string {
	var b strings.Builder
	if len(o.Dots) == 0 {
		return o.Note
	}
	for _, d := range o.Dots {
		state := d.Status
		if d.Paused {
			state = "paused"
		}
		fmt.Fprintf(&b, "%s: %s (%s, %s, %d/%d calls) %s\n", d.Slug, d.Name, d.Mode, state, d.Calls, d.Budget, d.PurposeSummary)
	}
	b.WriteString(o.Note)
	return b.String()
}

func renderAsk(o AskDotOut) string {
	if !o.Relevant || strings.TrimSpace(o.Message) == "" {
		return fmt.Sprintf("[◆ %s] nothing to add from its purpose (%s)\n%s", o.Name, cost(o.CostUSD, o.CostKnown), o.Note)
	}
	return fmt.Sprintf("[◆ %s] (%s) %s\n(%s)\n%s", o.Name, o.Severity, o.Message, cost(o.CostUSD, o.CostKnown), o.Note)
}

func renderCouncil(o AskCouncilOut) string {
	return fmt.Sprintf("[⊕ Council] %d members\n%s\n(about $%.4f)\n%s", len(o.Members), o.Digest, o.CostUSD, o.Note)
}

func renderQueue(o ListQueueOut) string {
	var b strings.Builder
	if len(o.Cards) == 0 {
		b.WriteString("The dots' queue is empty.\n")
	}
	for _, c := range o.Cards {
		fmt.Fprintf(&b, "#%s %s (%s, turn %d, %s): %s\n", c.ID, c.Dot, c.Severity, c.Turn, c.Status, c.Body)
	}
	if len(o.Cards) < o.Total {
		fmt.Fprintf(&b, "%d more not shown.\n", o.Total-len(o.Cards))
	}
	b.WriteString(o.Note)
	return b.String()
}

func renderMemory(o ReadMemoryOut) string {
	var b strings.Builder
	if len(o.Entries) == 0 {
		b.WriteString("No matching memory.\n")
	}
	for _, e := range o.Entries {
		fmt.Fprintf(&b, "[%s] %s: %s\n", e.ID, e.Kind, e.Text)
	}
	if len(o.Entries) < o.Total {
		fmt.Fprintf(&b, "%d more not shown (narrow with query, scope or dot).\n", o.Total-len(o.Entries))
	}
	if o.Note != "" {
		b.WriteString(o.Note + "\n")
	}
	return b.String()
}
