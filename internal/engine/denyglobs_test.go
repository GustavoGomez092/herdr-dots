package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
)

// Ruling P10-R7 (L20): while tool_deny_globs holds an invalid pattern, a
// review that would read the repository runs without repository tools. The
// same rig with a valid list keeps them.
func TestInvalidDenyGlobTurnsRepoToolsOff(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		d := dot("Security", dots.ModeQueue)
		d.Context.RepoRead = true
		r := newRigWith(t, func(dp *Deps) {
			if invalid {
				dp.Config.InvalidDenyGlobs = []string{"[unclosed"}
			}
		}, d)
		r.readers["s1"].add(transcript.Turn{User: "add login"})
		r.be.set(critical)
		r.attach(t, "security")
		eventually(t, "a review", func() bool { return r.be.n() >= 1 })
		if got := r.be.last().RepoRead; got == invalid {
			t.Fatalf("invalid glob=%v: request RepoRead=%v", invalid, got)
		}
	}
}

func TestInvalidDenyGlobRefusesDrafts(t *testing.T) {
	r := newRigWith(t, func(dp *Deps) {
		dp.Config.WriteEnabled = true
		dp.Config.InvalidDenyGlobs = []string{"[unclosed"}
	}, dot("Security", dots.ModeQueue))
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix it", ""); !errors.Is(err, ErrDenyGlobsInvalid) {
		t.Fatalf("Draft: %v, want ErrDenyGlobsInvalid", err)
	}
}

// The developer hears about it once per stretch of bad settings: one log
// line and one toast, not one per reload.
func TestInvalidDenyGlobIsLoggedAndToastedOnce(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	r := newRigWith(t, func(dp *Deps) {
		dp.Config.InvalidDenyGlobs = []string{"[unclosed"}
		dp.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	}, dot("Security", dots.ModeQueue))
	r.e.noteDenyConfig(r.e.config()) // a reload with the same bad settings
	toasts := func() int {
		n := 0
		for _, c := range r.srv.Calls("notification.show") {
			if strings.Contains(fmt.Sprint(c.Params["body"]), "tool_deny_globs") {
				n++
			}
		}
		return n
	}
	eventually(t, "the toast", func() bool { return toasts() >= 1 })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := 0
	for _, l := range logs {
		if strings.Contains(l, "tool_deny_globs") {
			n++
		}
	}
	mu.Unlock()
	if n != 1 || toasts() != 1 {
		t.Fatalf("%d log lines and %d toasts, want one each", n, toasts())
	}
}

// Research is off too, and the roster says why instead of a countdown.
func TestInvalidDenyGlobTurnsResearchOff(t *testing.T) {
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.InvalidDenyGlobs = []string{"[unclosed"} }}, researchDot("Security"))
	r.reader().add(transcript.Turn{User: "add login", Assistant: "edited internal/auth/session.go"})
	r.reader().add(transcript.Turn{User: "add logout"})
	r.attach(t, "security")
	r.idlePeriod(t)
	eventually(t, "the roster hint", func() bool { return r.dotState("security").Next == researchOffHint })
	r.clock.Add(11 * time.Minute)
	time.Sleep(100 * time.Millisecond)
	if n := len(r.be.runs(backend.PassResearch)); n != 0 {
		t.Fatalf("%d research runs with an invalid deny glob", n)
	}
}
