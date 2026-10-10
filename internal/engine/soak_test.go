//go:build soak && (darwin || linux)

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
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

	soakBurstTurns = 30 // turns burst into one pane
	soakBurstGap   = 200 * time.Millisecond
	soakBurstEvery = 5 * time.Minute // after the first, at the start
	soakSettle     = 3 * time.Second // after a burst's last turn, for its runs to finish
)

// soakBody pads every finding, so a pane with MaxCardsPerPane cards is over
// the snapshot byte budget and Trim's byte path runs, not only its card cap.
var soakBody = strings.Repeat("The error path returns before the lock is released. ", 45)

// soakBackend flags something critical on every odd call, for as long as the
// soak runs: half the runs make a card.
type soakBackend struct{ calls, flagged atomic.Int64 }

func (b *soakBackend) Kind() string                { return "claude-cli" }
func (b *soakBackend) Check(context.Context) error { return nil }
func (b *soakBackend) Run(context.Context, backend.Request) (backend.Result, error) {
	n := b.calls.Add(1)
	v := backend.Verdict{Relevant: false, Severity: "info"}
	if n%2 == 1 {
		b.flagged.Add(1)
		v = backend.Verdict{Relevant: true, Severity: "critical", Message: fmt.Sprintf("Finding %d. %s", n, soakBody)}
	}
	return backend.Result{Verdict: v, InputTokens: 10, OutputTokens: 5}, nil
}

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

// TestSoak runs 12 scripted dots on 4 panes. A turn ends every 3 seconds,
// round robin over the panes, and half the runs flag something (soakBackend).
// At the start and every 5 minutes, 30 turns burst into one pane, 200 ms
// apart: its queue passes MaxCardsPerPane and the byte budget, so Trim and
// broadcast coalescing both run, and the broadcast rate is measured over the
// burst. A simulated developer sends or dismisses every pending card each
// minute. Then the panes go quiet and the idle engine's CPU is measured.
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
	be := &soakBackend{}
	r := newRigWith(t, func(d *Deps) {
		d.Config.SessionBudgetCalls = 1 << 30
		d.Config.MaxParallelRuns = 3
		d.PollInterval = soakEnv("SOAK_POLL", 0) // 0: the engine's own default (3 s), as in production
		d.Backend = func(string) (backend.Backend, error) { return be, nil }
	}, ds...)
	r.srv.DiscardCalls() // the fake Herdr's call log would grow the test's memory, not the engine's
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
	var sent, biggest, mostCards, mostHidden atomic.Int64
	quit, readerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case s := <-ch:
				sent.Add(1)
				if b, _ := json.Marshal(s); int64(len(b)) > biggest.Load() {
					biggest.Store(int64(len(b)))
				}
				for _, p := range s.Panes {
					if n := int64(len(p.Cards) + p.HiddenCards); n > mostCards.Load() {
						mostCards.Store(n)
					}
					if n := int64(p.HiddenCards); n > mostHidden.Load() {
						mostHidden.Store(n)
					}
				}
			case <-quit:
				return
			}
		}
	}()
	defer func() { stop(); close(quit); <-readerDone }()
	startGoroutines := runtime.NumGoroutine()

	turns := 0
	turnOn := func(i int) {
		r.mu.Lock()
		rd := r.readers[fmt.Sprintf("s%d", i+1)]
		r.mu.Unlock()
		rd.add(transcript.Turn{User: fmt.Sprintf("change %d", turns), Assistant: "done"})
		r.srv.Push(panes[i], herdr.StatusWorking)
		r.srv.Push(panes[i], herdr.StatusIdle)
		turns++
	}
	var bursts int
	var worstRate float64 // broadcasts a second, the highest of any burst
	burst := func() {
		cards0, sent0, start := be.flagged.Load(), sent.Load(), time.Now()
		for i := 0; i < soakBurstTurns; i++ {
			turnOn(0)
			time.Sleep(soakBurstGap)
		}
		time.Sleep(soakSettle)
		window, n := time.Since(start), sent.Load()-sent0
		rate := float64(n) / window.Seconds()
		worstRate = max(worstRate, rate)
		bursts++
		t.Logf("burst %d: %d cards made, %d broadcasts in %v (%.1f a second)", bursts, be.flagged.Load()-cards0, n, window.Round(time.Millisecond), rate)
		if limit := int64(window/minBroadcastGap) + 1; n > limit {
			t.Errorf("burst %d: %d broadcasts in %v; at most 10 a second allows %d", bursts, n, window, limit)
		}
	}

	t.Logf("busy phase %v, then idle %v", busy, idle)
	end := time.Now().Add(busy)
	nextTidy := time.Now().Add(time.Minute)
	nextBurst := time.Now()
	for i := 0; time.Now().Before(end); i++ {
		if time.Now().After(nextBurst) {
			burst()
			nextBurst = time.Now().Add(soakBurstEvery)
		}
		turnOn(i % soakPanes)
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
	// SOAK_HEAPPROFILE=FILE writes the live heap here, while the engine still
	// runs (a -memprofile is written after teardown and shows nothing held).
	if hp := os.Getenv("SOAK_HEAPPROFILE"); hp != "" {
		runtime.GC()
		if f, err := os.Create(hp); err == nil {
			_ = pprof.WriteHeapProfile(f)
			f.Close()
		}
		t.Logf("heap profile written to %s (queue holds %d cards)", hp, len(r.q.All()))
	}
	cpu0, wall0 := cpuTime(), time.Now()
	time.Sleep(idle)
	idleCPU := float64(cpuTime()-cpu0) / float64(time.Since(wall0))
	rss := peakRSS()
	goroutines := runtime.NumGoroutine()
	snap, _ := json.Marshal(r.e.Snapshot())
	t.Logf("runs %d, cards %d, most cards on a pane %d (%d hidden at most), broadcasts %d, biggest snapshot %d B, final snapshot %d B, worst burst %.1f broadcasts a second, peak RSS %.1f MB, idle CPU %.3f%%, goroutines %d -> %d",
		be.calls.Load(), be.flagged.Load(), mostCards.Load(), mostHidden.Load(), sent.Load(), biggest.Load(), len(snap), worstRate,
		float64(rss)/(1<<20), idleCPU*100, startGoroutines, goroutines)

	if mostHidden.Load() == 0 || mostCards.Load() <= view.MaxCardsPerPane {
		t.Errorf("no pane passed %d cards (most %d, hidden %d): the soak never exercised Trim", view.MaxCardsPerPane, mostCards.Load(), mostHidden.Load())
	}
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
