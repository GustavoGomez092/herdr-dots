package state

import (
	"bytes"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform/platformtest"
)

func dailyAt(t *testing.T) (*Daily, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state", "daily.json")
	d, err := OpenDaily(p)
	if err != nil {
		t.Fatal(err)
	}
	return d, p
}

func TestDailyCountsRunsAndCost(t *testing.T) {
	d, p := dailyAt(t)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	for _, c := range []float64{0.10, 0.25, math.NaN(), -1, 0} {
		if err := d.Add("security", RunResearch, c, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.Add("security", RunScheduled, 0.02, now)
	day, err := d.Today(now)
	if err != nil {
		t.Fatal(err)
	}
	if day.Count("security", RunResearch) != 5 || day.Count("security", RunScheduled) != 1 || day.Count("frontend", RunResearch) != 0 || day.Total() != 6 {
		t.Fatalf("counts %+v", day.Runs)
	}
	if math.Abs(day.CostUSD-0.37) > 1e-9 {
		t.Fatalf("cost %v, want 0.37 (NaN and negative costs add nothing)", day.CostUSD)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), `"version": 1`) || !strings.Contains(string(raw), `"day": "2026-10-21"`) {
		t.Fatalf("file %s", raw)
	}
	platformtest.AssertPrivateFile(t, p)
}

func TestDailyRollsOverAtLocalMidnight(t *testing.T) {
	d, _ := dailyAt(t)
	loc := time.FixedZone("UTC-5", -5*3600)
	late := time.Date(2026, 10, 21, 23, 59, 0, 0, loc)
	_ = d.Add("security", RunResearch, 1.00, late)
	if day, _ := d.Today(late); math.Abs(day.CostUSD-1.00) > 1e-9 {
		t.Fatalf("before midnight: %+v", day)
	}
	early := time.Date(2026, 10, 22, 0, 1, 0, 0, loc)
	day, err := d.Today(early)
	if err != nil || day.CostUSD != 0 || day.Total() != 0 || day.Date != "2026-10-22" {
		t.Fatalf("after midnight: %+v %v", day, err)
	}
	_ = d.Add("security", RunResearch, 0.05, early)
	if day, _ := d.Today(early); day.Count("security", RunResearch) != 1 || math.Abs(day.CostUSD-0.05) > 1e-9 {
		t.Fatalf("new day: %+v", day)
	}
}

// A corrupt daily file must never reset the cap to zero. It is moved aside
// and the rest of that local day is blocked (budget reached); the next local
// day starts clean. The failure is logged once.
func TestDailyCorruptFileBlocksTheDay(t *testing.T) {
	d, p := dailyAt(t)
	_ = os.WriteFile(p, []byte("{not json"), 0o600)
	var logged int
	d.Logf = func(string, ...any) { logged++ }
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	day, err := d.Today(now)
	if err != nil || !day.Blocked || day.Date != "2026-10-21" {
		t.Fatalf("day %+v err %v", day, err)
	}
	if m, _ := filepath.Glob(p + ".corrupt-*"); len(m) != 1 {
		t.Fatalf("moved aside: %v", m)
	}
	if err := d.Add("x", RunResearch, 0.5, now); err != nil {
		t.Fatal(err)
	}
	if day, _ = d.Today(now); !day.Blocked || day.Count("x", RunResearch) != 1 {
		t.Fatalf("Add must keep the day blocked: %+v", day)
	}
	_, _ = d.Today(now)
	if logged != 1 {
		t.Fatalf("logged %d times, want exactly 1", logged)
	}
	platformtest.AssertPrivateFile(t, p)
	next, err := d.Today(now.AddDate(0, 0, 1))
	if err != nil || next.Blocked || next.Total() != 0 {
		t.Fatalf("next day: %+v %v", next, err)
	}
}

// A clock stepping backwards must neither restart nor overwrite a later day.
func TestDailyClockBackwardsKeepsLaterDay(t *testing.T) {
	d, p := dailyAt(t)
	loc := time.FixedZone("UTC-5", -5*3600)
	day2 := time.Date(2026, 10, 22, 0, 30, 0, 0, loc)
	day1 := time.Date(2026, 10, 21, 23, 50, 0, 0, loc)
	_ = d.Add("security", RunResearch, 0.40, day2)
	if err := d.Add("security", RunResearch, 0.10, day1); err != nil {
		t.Fatal(err)
	}
	got, err := d.Today(day1)
	if err != nil || got.Date != "2026-10-22" || got.Count("security", RunResearch) != 2 || math.Abs(got.CostUSD-0.50) > 1e-9 {
		t.Fatalf("backwards: %+v %v", got, err)
	}
	got, _ = d.Today(day2.Add(time.Hour))
	if got.Date != "2026-10-22" || got.Count("security", RunResearch) != 2 || math.Abs(got.CostUSD-0.50) > 1e-9 {
		t.Fatalf("forward again: %+v", got)
	}
	if raw, _ := os.ReadFile(p); !strings.Contains(string(raw), `"day": "2026-10-22"`) {
		t.Fatalf("later date overwritten: %s", raw)
	}
}

func TestDailyRollsOverOnDSTDay(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	d, _ := dailyAt(t)
	// 2026-11-01 is a 25-hour day in New York (clocks fall back at 02:00).
	before := time.Date(2026, 11, 1, 0, 30, 0, 0, loc)
	late := time.Date(2026, 11, 1, 23, 30, 0, 0, loc)
	next := time.Date(2026, 11, 2, 0, 30, 0, 0, loc)
	_ = d.Add("security", RunResearch, 1, before)
	if day, _ := d.Today(late); day.Date != "2026-11-01" || day.Total() != 1 {
		t.Fatalf("same DST day: %+v", day)
	}
	if day, _ := d.Today(next); day.Date != "2026-11-02" || day.Total() != 0 || day.CostUSD != 0 {
		t.Fatalf("next day: %+v", day)
	}
}

func TestDailyCostIsExactInMicrodollars(t *testing.T) {
	d, _ := dailyAt(t)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	for i := 0; i < 1000; i++ {
		if err := d.Add("security", RunResearch, 0.001, now); err != nil {
			t.Fatal(err)
		}
	}
	if day, _ := d.Today(now); day.CostUSD != 1.0 {
		t.Fatalf("cost %v, want exactly 1.0", day.CostUSD)
	}
}

func TestDailyLoadsOldFloatOnlyFile(t *testing.T) {
	d, p := dailyAt(t)
	_ = os.WriteFile(p, []byte(`{"version":1,"day":"2026-10-21","runs":{"a":{"research":2}},"cost_usd":0.37}`), 0o600)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	_ = d.Add("a", RunResearch, 0.03, now)
	day, err := d.Today(now)
	if err != nil || day.Count("a", RunResearch) != 3 || day.CostUSD != 0.40 {
		t.Fatalf("%+v %v", day, err)
	}
}

func TestDailyRefusesNewerVersion(t *testing.T) {
	d, p := dailyAt(t)
	_ = os.WriteFile(p, []byte(`{"version":2,"day":"2026-10-21"}`), 0o600)
	if _, err := d.Today(time.Now()); !errors.Is(err, ErrDailyVersion) {
		t.Fatalf("err %v", err)
	}
	if err := d.Add("x", RunCommit, 0, time.Now()); !errors.Is(err, ErrDailyVersion) {
		t.Fatalf("add err %v", err)
	}
}

// Two engines in two Herdr sessions are two processes: the file lock must
// serialize them with no lost counts. The test binary re-runs itself.
func TestDailyCrossProcessAddsLoseNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daily.json")
	const procs, each = 2, 50
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*bytes.Buffer, procs)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDailyXProcChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), "DAILY_XPROC_PATH="+p, "DAILY_XPROC_N="+strconv.Itoa(each))
		outs[i] = &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = outs[i], outs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("child %d: %v\n%s", i, err, outs[i])
		}
	}
	d, _ := OpenDaily(p)
	day, err := d.Today(time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if n := day.Count("security", RunResearch); n != procs*each {
		t.Fatalf("%d runs, want %d (lost updates across processes)", n, procs*each)
	}
	if math.Abs(day.CostUSD-float64(procs*each)*0.01) > 1e-6 {
		t.Fatalf("cost %v", day.CostUSD)
	}
}

// TestDailyXProcChild is the writer process for the test above.
func TestDailyXProcChild(t *testing.T) {
	p := os.Getenv("DAILY_XPROC_PATH")
	if p == "" {
		t.Skip("helper process for TestDailyCrossProcessAddsLoseNothing")
	}
	n, _ := strconv.Atoi(os.Getenv("DAILY_XPROC_N"))
	d, err := OpenDaily(p)
	if err != nil {
		t.Fatal(err)
	}
	d.Timeout = 60 * time.Second
	for i := 0; i < n; i++ {
		if err := d.Add("security", RunResearch, 0.01, time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)); err != nil {
			t.Fatal(err)
		}
	}
}
