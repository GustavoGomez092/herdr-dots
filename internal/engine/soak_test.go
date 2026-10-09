//go:build soak && (darwin || linux)

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Soak budgets (spec F9).
const (
	soakMaxIdleCPU = 0.01     // idle engine CPU, averaged over the idle window
	soakMaxRSS     = 80 << 20 // peak resident memory
	soakDots       = 12
	soakPanes      = 4
)

func soakEnv(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func peakRSS() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return int64(ru.Maxrss) * rssUnit
}

// TestSoak runs 12 scripted dots on 4 panes: turns end every few seconds on
// a random pane, half the runs flag something, and a simulated developer
// sends or dismisses cards every minute. Then the panes go quiet and the
// idle engine's CPU is measured.
//
//	SOAK_DURATION=8h SOAK_IDLE=10m go test -tags soak -run TestSoak -timeout 9h -v ./internal/engine/
func TestSoak(t *testing.T) {
	busy := soakEnv("SOAK_DURATION", 15*time.Minute)
	idle := soakEnv("SOAK_IDLE", min(10*time.Minute, busy/3))
	var ds []dots.Dot
	var slugs []string
	for i := 0; i < soakDots; i++ {
		d := dot(fmt.Sprintf("Soak %02d", i), dots.ModeQueue)
		d.Budget.MaxCallsPerSession = 1 << 30
		ds = append(ds, d)
		slugs = append(slugs, dots.Slugify(d.Name))
	}
	r := newRigWith(t, func(d *Deps) {
		d.Config.SessionBudgetCalls = 1 << 30
		d.Config.MaxParallelRuns = 3
		d.PollInterval = time.Second
	}, ds...)
	r.be.set(critical, backend.Verdict{Relevant: false, Severity: "info"})
	panes := make([]string, soakPanes)
	for i := range panes {
		panes[i] = fmt.Sprintf("w1:p%d", i+1)
		sess := fmt.Sprintf("s%d", i+1)
		r.srv.SetPane(herdr.PaneInfo{PaneID: panes[i], Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
			AgentSession: &herdr.AgentSession{Kind: "id", Value: sess}})
		r.setReader(sess, &fakeReader{})
		if err := r.e.Attach(context.Background(), panes[i], slugs); err != nil {
			t.Fatal(err)
		}
	}
	ch, stop := r.e.Watch()
	defer stop()
	var sent, biggest atomic.Int64
	go func() {
		for s := range ch {
			sent.Add(1)
			if b, _ := json.Marshal(s); int64(len(b)) > biggest.Load() {
				biggest.Store(int64(len(b)))
			}
		}
	}()
	startGoroutines := runtime.NumGoroutine()

	t.Logf("busy phase %v, then idle %v", busy, idle)
	end := time.Now().Add(busy)
	nextTidy := time.Now().Add(time.Minute)
	for turn := 0; time.Now().Before(end); turn++ {
		p := panes[turn%soakPanes]
		r.mu.Lock()
		rd := r.readers[fmt.Sprintf("s%d", turn%soakPanes+1)]
		r.mu.Unlock()
		rd.add(transcript.Turn{User: fmt.Sprintf("change %d", turn), Assistant: "done"})
		r.srv.Push(p, herdr.StatusWorking)
		r.srv.Push(p, herdr.StatusIdle)
		if time.Now().After(nextTidy) { // the developer works through the queue
			for i, c := range r.q.All() {
				if c.Status == "pending" {
					if i%2 == 0 {
						_ = r.e.Dismiss(c.ID)
					} else {
						_ = r.e.SendCards([]string{c.ID})
					}
				}
			}
			nextTidy = time.Now().Add(time.Minute)
		}
		time.Sleep(3 * time.Second)
	}

	time.Sleep(30 * time.Second) // let the last runs finish
	cpu0, wall0 := cpuTime(), time.Now()
	time.Sleep(idle)
	idleCPU := float64(cpuTime()-cpu0) / float64(time.Since(wall0))
	rss := peakRSS()
	goroutines := runtime.NumGoroutine()
	snap, _ := json.Marshal(r.e.Snapshot())
	t.Logf("runs %d, broadcasts %d, biggest snapshot %d B, final snapshot %d B, peak RSS %.1f MB, idle CPU %.3f%%, goroutines %d -> %d",
		r.be.n(), sent.Load(), biggest.Load(), len(snap), float64(rss)/(1<<20), idleCPU*100, startGoroutines, goroutines)

	if idleCPU > soakMaxIdleCPU {
		t.Errorf("idle CPU %.3f%% is over the 1%% budget", idleCPU*100)
	}
	if rss > soakMaxRSS {
		t.Errorf("peak RSS %.1f MB is over the 80 MB budget", float64(rss)/(1<<20))
	}
	if b := biggest.Load(); b > view.MaxSnapshotBytes {
		t.Errorf("a snapshot of %d bytes was broadcast; the budget is %d", b, view.MaxSnapshotBytes)
	}
	if maxSent := int64((busy+idle+30*time.Second)/minBroadcastGap) + 1; sent.Load() > maxSent {
		t.Errorf("%d broadcasts in %v: more than 10 a second", sent.Load(), busy+idle)
	}
	if goroutines > startGoroutines+10 {
		t.Errorf("goroutines grew from %d to %d", startGoroutines, goroutines)
	}
}
