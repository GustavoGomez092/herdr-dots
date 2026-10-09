package engine

import (
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

func baseInput() FilterInput {
	d := dots.New("Security")
	d.Purpose = "p"
	return FilterInput{
		Dot:        d,
		Att:        state.DotAttachment{Slug: "security"},
		NewTurns:   []transcript.Turn{{Index: 1, User: "hi", Tools: []transcript.ToolUse{{Name: "Edit", Paths: []string{"/repo/web/app.tsx"}}}}},
		PaneBudget: 120,
	}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*FilterInput)
		want Reason
	}{
		{"runs by default", func(*FilterInput) {}, ReasonRun},
		{"paused all", func(in *FilterInput) { in.PausedAll = true }, ReasonPausedAll},
		{"paused", func(in *FilterInput) { in.Att.Paused = true }, ReasonPaused},
		{"observe never runs on turns", func(in *FilterInput) { in.Dot.Delivery.Mode = dots.ModeObserve }, ReasonObserve},
		{"on_turn off", func(in *FilterInput) { in.Dot.Trigger.OnTurn = false }, ReasonOff},
		{"only own dot turns", func(in *FilterInput) { in.NewTurns[0].FromDots = []string{"Security"} }, ReasonFromDot},
		{"other dot's turn still counts", func(in *FilterInput) { in.NewTurns[0].FromDots = []string{"Frontend"} }, ReasonRun},
		{"cooldown", func(in *FilterInput) { in.Att.CooldownLeft = 1 }, ReasonCooldown},
		{"glob miss", func(in *FilterInput) { in.Dot.Trigger.FileGlobs = []string{"**/*.go"} }, ReasonGlobs},
		{"glob hit absolute path", func(in *FilterInput) { in.Dot.Trigger.FileGlobs = []string{"**/*.tsx"} }, ReasonRun},
		{"glob hit base name", func(in *FilterInput) { in.Dot.Trigger.FileGlobs = []string{"*.tsx"} }, ReasonRun},
		{"dot budget", func(in *FilterInput) { in.Dot.Budget.MaxCallsPerSession = 2; in.DotCalls = 2 }, ReasonBudget},
		{"session budget", func(in *FilterInput) { in.PaneCalls = 120 }, ReasonSessionBudget},
	}
	for _, c := range cases {
		in := baseInput()
		c.mod(&in)
		got := Decide(in)
		if got.Reason != c.want || got.Run != (c.want == ReasonRun) {
			t.Errorf("%s: got %+v, want %s", c.name, got.Reason, c.want)
		}
	}
}

func TestDecideDropsOwnTurnsFromContext(t *testing.T) {
	in := baseInput()
	in.NewTurns = append(in.NewTurns, transcript.Turn{Index: 2, FromDots: []string{"Security"}})
	got := Decide(in)
	if !got.Run || len(got.Turns) != 1 || got.Turns[0].Index != 1 {
		t.Fatalf("got %+v", got)
	}
}
