package pane

import (
	"fmt"
	"strings"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// Proactive dots (Phase 8) in the Dots pane: roster hints, background-run
// status, the "while you were away" line, today's background spend, and
// card headers labeled by what started them.

// hasParts reports a card whose member answers expand with space: a council
// card or a "while you were away" digest.
func hasParts(c queue.Card) bool { return c.Kind == queue.KindCouncil || c.Kind == queue.KindDigest }

// dotStatus is the roster status column: a background run in flight wins,
// then a memory pass, then the dot's status; a proactive hint follows.
func dotStatus(d view.DotState) string {
	status := d.Status
	switch {
	case d.Background == state.RunResearch:
		status = style.Sym("◌") + " researching" + style.Sym("…")
	case d.Background == state.RunScheduled:
		status = style.Sym("◌") + " scheduled run" + style.Sym("…")
	case d.Background == state.RunCommit:
		status = style.Sym("◌") + " reviewing commit" + style.Sym("…")
	case d.Learning:
		status = style.Sym("◌") + " learning" + style.Sym("…")
	}
	if d.Next != "" {
		status += " · " + textutil.SanitizeModelText(d.Next)
	}
	return status
}

// cardWhere is the card header's "turn N", or a background card's label
// ("commit a1b2c3d", "scheduled Mon 09:00", "2 dots · 42 min").
func cardWhere(c queue.Card) string {
	if c.Origin != "" && c.Label != "" {
		return textutil.SanitizeModelText(c.Label)
	}
	return fmt.Sprintf("turn %d", c.Turn)
}

// todayPart is the header's background spend, once anything ran today.
func todayPart(s view.Snapshot) string {
	if s.TodayRuns == 0 {
		return ""
	}
	return fmt.Sprintf("  today $%.2f/%.2f", s.TodayUSD, s.DailyBudgetUSD)
}

// awayLine says research is waiting for the developer's return.
func awayLine(p view.Pane) string {
	if p.Away == nil || p.Away.Pending == 0 {
		return ""
	}
	noun := "results"
	if p.Away.Pending == 1 {
		noun = "result"
	}
	return fmt.Sprintf("%s %d research %s waiting (since %s); they become one card on your next turn",
		style.Sym("☾"), p.Away.Pending, noun, p.Away.Since.Local().Format("15:04"))
}

// proactiveOutcome reports a background run's feed outcome: its line shows
// the run's label and message, never Phase 4's cost tail (costPart).
func proactiveOutcome(outcome string) bool {
	switch outcome {
	case state.OutcomeResearch, state.OutcomeScheduled, state.OutcomeCommit:
		return true
	}
	return false
}

// proactiveFeed shows a background run's detail ("research: nothing new").
func proactiveFeed(e state.FeedEntry) (string, bool) {
	switch e.Outcome {
	case state.OutcomeResearch, state.OutcomeScheduled, state.OutcomeCommit:
		if e.Detail != "" {
			d, _, _ := strings.Cut(e.Detail, " · cost=") // the pane keeps lines short
			return textutil.SanitizeModelText(d), true
		}
	}
	return "", false
}
