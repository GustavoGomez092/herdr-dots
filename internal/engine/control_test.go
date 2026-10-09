package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

var _ ipc.Handler = (*Engine)(nil)

func TestSummonObserveDotAlwaysQueuesACard(t *testing.T) {
	r := newRig(t, dot("Copy", dots.ModeObserve))
	r.readers["s1"].add(transcript.Turn{User: "write hero copy"})
	r.attach(t, "copy")
	r.be.set(backend.Verdict{Relevant: true, Severity: "info", Message: "Lead with the outcome."})
	if err := r.e.Summon(context.Background(), pane, "copy", "is the headline good?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "card from summon", func() bool { return len(r.q.Pending(pane)) == 1 })
	req := r.be.last()
	if !req.Force || req.Question != "is the headline good?" || req.Turns[0].User != "write hero copy" {
		t.Fatalf("request %+v", req)
	}
	if c := r.q.Pending(pane)[0]; c.Mode != "queue" {
		t.Fatalf("summon cards are queue cards, got %s", c.Mode)
	}
	if err := r.e.Summon(context.Background(), pane, "nope", ""); err == nil {
		t.Fatal("unknown dot")
	}
	if err := r.e.Summon(context.Background(), "w9:p9", "copy", ""); err == nil {
		t.Fatal("unattached pane")
	}
}

func TestThreeFailuresStopDotUntilReset(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.be.fail(errors.New("claude is not logged in: run `claude` and type /login"))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "security")
	eventually(t, "run 1", func() bool { return r.be.n() == 1 })
	eventually(t, "run 1 done", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Dots[0].Status == view.StatusError })
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "run 2", func() bool { return r.be.n() == 2 })
	eventually(t, "run 2 done", func() bool { a, _ := r.atts.Get(pane); return a.Find("security").Failures == 2 })
	r.turnEnds(t, transcript.Turn{User: "three"})
	eventually(t, "stopped", func() bool { a, _ := r.atts.Get(pane); return a.Find("security").Paused })
	eventually(t, "stop toast", func() bool {
		for _, c := range r.srv.Calls("notification.show") {
			if strings.Contains(c.Params["body"].(string), "stopped after 3 failures") {
				return true
			}
		}
		return false
	})
	if p, _ := r.e.Snapshot().Pane(pane); p.Dots[0].Status != view.StatusError {
		t.Fatalf("a stopped dot shows error, got %s", p.Dots[0].Status)
	}
	if err := r.e.Reset(pane, "security"); err != nil {
		t.Fatal(err)
	}
	a, _ := r.atts.Get(pane)
	if da := a.Find("security"); da.Paused || da.Failures != 0 {
		t.Fatalf("reset %+v", da)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.Dots[0].Status != view.StatusIdle {
		t.Fatalf("status after reset %s", p.Dots[0].Status)
	}
}

func TestPauseAllStopsRuns(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.e.TogglePauseAll()
	if !r.e.Snapshot().PausedAll {
		t.Fatal("paused all")
	}
	r.turnEnds(t, transcript.Turn{User: "x"})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "security") == 1 })
	if r.be.n() != 0 {
		t.Fatal("no runs while paused")
	}
	r.e.TogglePauseAll()
	if r.e.Snapshot().PausedAll {
		t.Fatal("resumed")
	}
}

func TestSetPausedAndSetMode(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	if err := r.e.SetPaused(pane, "security", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.Dots[0].Status != view.StatusPaused {
		t.Fatalf("status %s", p.Dots[0].Status)
	}
	if err := r.e.SetPaused(pane, "nope", true); err == nil {
		t.Fatal("unknown dot")
	}
	if err := r.e.SetMode("security", dots.ModeSuggest); err != nil {
		t.Fatal(err)
	}
	if len(r.saved) != 1 || r.saved[0].Delivery.Mode != dots.ModeSuggest {
		t.Fatalf("saved %+v", r.saved)
	}
	if err := r.e.SetMode("security", "shout"); err == nil {
		t.Fatal("invalid mode")
	}
}

func TestHandleRoutesCommands(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	ctx := context.Background()
	raw, _ := json.Marshal(ipc.AttachArgs{PaneID: pane, Slugs: []string{"security"}})
	if err := r.e.Handle(ctx, ipc.CmdAttach, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.atts.Get(pane); !ok {
		t.Fatal("attach via Handle")
	}
	raw, _ = json.Marshal(ipc.DotArgs{PaneID: pane, Slug: "security"})
	if err := r.e.Handle(ctx, ipc.CmdDetach, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.atts.Get(pane); ok {
		t.Fatal("detaching the last dot removes the attachment")
	}
	if err := r.e.Handle(ctx, ipc.CmdAttach, nil); err == nil {
		t.Fatal("missing args must error")
	}
	if err := r.e.Handle(ctx, "bogus", nil); err == nil {
		t.Fatal("unknown command must error")
	}
	if err := r.e.Handle(ctx, ipc.CmdPing, nil); err != nil {
		t.Fatal(err)
	}
}
