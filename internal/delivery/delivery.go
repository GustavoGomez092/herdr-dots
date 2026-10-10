// Package delivery sends dot messages into a main agent, but only when that
// cannot collide with the agent's work or the developer's typing.
package delivery

import (
	"context"
	"errors"
	"strings"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/transcript"
)

const MaxInjectChars = 2000

// ErrPatchInject: patch cards are never injected; they need the user's approval.
var ErrPatchInject = errors.New("patch cards are never injected")

type Target interface {
	PaneGet(ctx context.Context, paneID string) (herdr.PaneInfo, error)
	AgentPrompt(ctx context.Context, target, text string) error
}

type Gate int

const (
	Now     Gate = iota // deliver now
	Busy                // agent not idle: wait for the next idle
	Focused             // inject only: developer is in that pane
)

func (g Gate) String() string {
	switch g {
	case Now:
		return "now"
	case Busy:
		return "busy"
	}
	return "focused"
}

func CanDeliver(p herdr.PaneInfo, requireUnfocused bool) Gate {
	if p.AgentStatus != herdr.StatusIdle && p.AgentStatus != herdr.StatusDone {
		return Busy
	}
	if requireUnfocused && p.Focused {
		return Focused
	}
	return Now
}

// Format puts each dot's header once, in first-seen order, followed by all of
// that dot's messages. A message line that looks like a dot header gets
// transcript.NeutralMarker in front, so only the real headers are parsed.
func Format(cards []queue.Card) string {
	var order []string
	groups := map[string][]queue.Card{}
	for _, c := range cards {
		if _, ok := groups[c.Dot]; !ok {
			order = append(order, c.Dot)
		}
		groups[c.Dot] = append(groups[c.Dot], c)
	}
	var parts []string
	for _, slug := range order {
		g := groups[slug]
		bodies := make([]string, len(g))
		for i, c := range g {
			bodies[i] = transcript.NeutraliseDotPrefixes(strings.TrimSpace(c.Body))
		}
		parts = append(parts, transcript.DotPrefix(g[0].Glyph, g[0].DotName)+" "+strings.Join(bodies, "\n\n"))
	}
	return strings.Join(parts, "\n\n")
}

func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

func Send(ctx context.Context, t Target, paneID string, cards []queue.Card, inject bool) (Gate, error) {
	if len(cards) == 0 {
		return Now, nil
	}
	if inject {
		for _, c := range cards {
			if c.Kind == queue.KindPatch {
				return Busy, ErrPatchInject
			}
		}
	}
	p, err := t.PaneGet(ctx, paneID)
	if err != nil {
		return Busy, err
	}
	if g := CanDeliver(p, inject); g != Now {
		return g, nil
	}
	text := Format(cards)
	if inject {
		text = TruncateRunes(text, MaxInjectChars)
	}
	return Now, t.AgentPrompt(ctx, paneID, text)
}
