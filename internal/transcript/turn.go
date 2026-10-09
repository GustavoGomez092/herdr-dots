// Package transcript turns an agent's conversation into Turns. A turn starts
// at a real user prompt and contains the agent's text and tool calls up to the
// next prompt.
package transcript

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	SourceClaude     = "claude"
	SourceCodex      = "codex"
	SourceScrollback = "scrollback"
)

// MaxFieldChars caps each user/assistant field sent to a dot.
const MaxFieldChars = 8000

type ToolUse struct {
	Name  string
	Paths []string
}

type Turn struct {
	Index     int // 1-based, stable for an append-only transcript
	User      string
	Assistant string
	Tools     []ToolUse
	FromDots  []string // dots whose delivered message is this turn's prompt
}

type Reader interface {
	Turns(ctx context.Context) ([]Turn, error)
	Source() string
}

func (t Turn) TouchedPaths() []string {
	var out []string
	for _, tu := range t.Tools {
		for _, p := range tu.Paths {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// CouncilName and CouncilGlyph identify a council delivery: "[⊕ Council dot] …".
const (
	CouncilName  = "Council"
	CouncilGlyph = "⊕"
)

// IsFromDot reports whether the named dot started this turn. A council
// delivery speaks for every dot, so a turn it started counts as started by
// all of them (the loop guard then skips it for every dot).
func (t Turn) IsFromDot(name string) bool {
	return slices.Contains(t.FromDots, name) || slices.Contains(t.FromDots, CouncilName)
}

func (t Turn) FromAnyDot() bool { return len(t.FromDots) > 0 }

// DotPrefix is the header delivery puts in front of a dot's message.
func DotPrefix(glyph, name string) string { return fmt.Sprintf("[%s %s dot]", glyph, name) }

var prefixRe = regexp.MustCompile(`(?m)^\[\S+ (.+?) dot\]`)

func FromDots(text string) []string {
	var out []string
	for _, m := range prefixRe.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// clip shortens s to n runes. keepTail keeps the end (conclusions) instead of
// the start.
func clip(s string, n int, keepTail bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if keepTail {
		return "…" + string(r[len(r)-n:])
	}
	return string(r[:n]) + "…"
}

func Format(turns []Turn) string {
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "### Turn %d\n", t.Index)
		if t.User != "" {
			fmt.Fprintf(&b, "USER:\n%s\n\n", clip(strings.TrimSpace(t.User), MaxFieldChars, false))
		}
		if t.Assistant != "" {
			fmt.Fprintf(&b, "AGENT:\n%s\n\n", clip(strings.TrimSpace(t.Assistant), MaxFieldChars, true))
		}
		if p := t.TouchedPaths(); len(p) > 0 {
			fmt.Fprintf(&b, "FILES TOUCHED: %s\n\n", strings.Join(p, ", "))
		}
	}
	return strings.TrimSpace(b.String())
}
