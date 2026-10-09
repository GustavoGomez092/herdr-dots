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
	if day, _ := d.Today(late); day.CostUSD != 1.00 {
		t.Fatalf("before midnight: %+v", day)
	}
	early := time.Date(2026, 10, 22, 0, 1, 0, 0, loc)
	day, err := d.Today(early)
	if err != nil || day.CostUSD != 0 || day.Total() != 0 || day.Date != "2026-10-22" {
		t.Fatalf("after midnight: %+v %v", day, err)
	}
	_ = d.Add("security", RunResearch, 0.05, early)
	if day, _ := d.Today(early); day.Count("security", RunResearch) != 1 || day.CostUSD != 0.05 {
		t.Fatalf("new day: %+v", day)
	}
}

// A corrupt daily file must never reset the cap to zero: that would make the
// budget unlimited. Today and Add both fail closed with ErrDailyCorrupt, the
// file is left in place for inspection, and the failure is logged.
func TestDailyCorruptFileFailsClosed(t *testing.T) {
	d, p := dailyAt(t)
	bad := []byte("{not json")
	_ = os.WriteFile(p, bad, 0o600)
	var logged int
	d.Logf = func(string, ...any) { logged++ }
	now := time.Now()
	if _, err := d.Today(now); !errors.Is(err, ErrDailyCorrupt) {
		t.Fatalf("Today err %v, want ErrDailyCorrupt", err)
	}
	if err := d.Add("x", RunResearch, 0.5, now); !errors.Is(err, ErrDailyCorrupt) {
		t.Fatalf("Add err %v, want ErrDailyCorrupt", err)
	}
	if logged == 0 {
		t.Fatal("corrupt file was not logged")
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, bad) {
		t.Fatalf("corrupt file was modified: %q", got)
	}
	if m, _ := filepath.Glob(p + ".corrupt-*"); len(m) != 0 {
		t.Fatalf("file must not be moved aside (that would reset the cap): %v", m)
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
	d, _ := OpenDaily(p)
	d.Timeout = 60 * time.Second
	for i := 0; i < n; i++ {
		if err := d.Add("security", RunResearch, 0.01, time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)); err != nil {
			t.Fatal(err)
		}
	}
}
