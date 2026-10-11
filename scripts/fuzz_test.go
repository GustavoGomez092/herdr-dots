package scripts

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fuzzTargets finds every native fuzz target in the module's Go test files,
// by package directory ("./internal/memory/").
func fuzzTargets(t *testing.T) map[string][]string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^func (Fuzz\w*)\(\w+ \*testing\.F\)`)
	got := map[string][]string{}
	for _, root := range []string{"internal", "cmd", "scripts"} {
		err := filepath.WalkDir(filepath.Join("..", root), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel("..", filepath.Dir(p))
			if err != nil {
				return err
			}
			pkg := "./" + filepath.ToSlash(rel) + "/"
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				got[pkg] = append(got[pkg], m[1])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return got
}

// jobBlock is the text of one job (two-space indented key) of a workflow.
func jobBlock(t *testing.T, workflow, job string) string {
	t.Helper()
	start := strings.Index(workflow, "\n  "+job+":\n")
	if start < 0 {
		t.Fatalf("no %s job", job)
	}
	rest := workflow[start+1:]
	if next := regexp.MustCompile(`\n  [A-Za-z0-9_-]+:\n`).FindStringIndex(rest[1:]); next != nil {
		rest = rest[:next[0]+1]
	}
	return rest
}

// The release fuzz job is a matrix: the union of its groups is every fuzz
// target in the module (a new target or package can't be left out), the
// total meets fuzz.sh's MIN_TARGETS, and each group's timeout keeps at least
// 30% headroom over its targets' fuzz time plus the builds.
func TestReleaseFuzzMatrixCoversEveryTarget(t *testing.T) {
	wf, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	job := jobBlock(t, string(wf), "fuzz")
	m := regexp.MustCompile(`(?m)^\s+timeout-minutes: (\d+)$`).FindStringSubmatch(job)
	if m == nil {
		t.Fatal("the fuzz job has no timeout-minutes")
	}
	timeout, _ := strconv.Atoi(m[1])
	if !regexp.MustCompile(`sh scripts/fuzz\.sh "\$FUZZ_TIME" \$\{\{ matrix\.packages \}\}`).MatchString(job) ||
		!strings.Contains(job, "FUZZ_TIME: ${{ github.event.inputs.fuzz_time || '10m' }}") {
		t.Fatal(`the fuzz job doesn't run sh scripts/fuzz.sh "$FUZZ_TIME" ${{ matrix.packages }} with FUZZ_TIME defaulting to 10m`)
	}
	// The headroom is checked at the release gate's time: the dispatch
	// input's default, which is also the tag push's time.
	m = regexp.MustCompile(`(?m)^\s+fuzz_time:\n(?:\s+\w+: .*\n)*?\s+default: "(\S+)"`).FindStringSubmatch(string(wf))
	if m == nil || m[1] != "10m" {
		t.Fatalf("the fuzz_time input must default to the 10m release gate: %v", m)
	}
	per, err := time.ParseDuration(m[1])
	if err != nil {
		t.Fatal(err)
	}
	sh, err := os.ReadFile("fuzz.sh")
	if err != nil {
		t.Fatal(err)
	}
	mt := regexp.MustCompile(`(?m)^MIN_TARGETS=(\d+)$`).FindSubmatch(sh)
	if mt == nil {
		t.Fatal("fuzz.sh has no MIN_TARGETS")
	}
	minTargets, _ := strconv.Atoi(string(mt[1]))

	targets := fuzzTargets(t)
	total := 0
	for _, ts := range targets {
		total += len(ts)
	}
	if total < minTargets {
		t.Fatalf("found %d fuzz targets, fuzz.sh expects at least %d", total, minTargets)
	}
	// Each matrix entry: packages, and optionally targets (a share of them).
	type group struct{ pkgs, targets []string }
	var groups []group
	for _, e := range regexp.MustCompile(`(?m)^\s+- group: `).Split(job, -1)[1:] {
		pm := regexp.MustCompile(`(?m)^\s+packages: (.+)$`).FindStringSubmatch(e)
		if pm == nil {
			t.Fatalf("a fuzz group has no packages: %q", e)
		}
		g := group{pkgs: strings.Fields(pm[1])}
		if tm := regexp.MustCompile(`(?m)^\s+targets: (.+)$`).FindStringSubmatch(e); tm != nil {
			g.targets = strings.Fields(tm[1])
		}
		groups = append(groups, g)
	}
	if len(groups) < 4 {
		t.Fatalf("the fuzz matrix has %d groups, want at least 4", len(groups))
	}
	if !strings.Contains(job, "FUZZ_TARGETS: ${{ matrix.targets }}") {
		t.Fatal("the fuzz job must pass matrix.targets to fuzz.sh as FUZZ_TARGETS")
	}
	seen := map[string]bool{} // "pkg Target"
	covered := 0
	for _, g := range groups {
		var run []string
		if len(g.targets) == 0 {
			for _, p := range g.pkgs {
				if len(targets[p]) == 0 {
					t.Errorf("fuzz group package %s has no fuzz target", p)
				}
				for _, tg := range targets[p] {
					run = append(run, p+" "+tg)
				}
			}
		} else {
			for _, tg := range g.targets {
				found := ""
				for _, p := range g.pkgs {
					if slices.Contains(targets[p], tg) {
						found = p + " " + tg
					}
				}
				if found == "" {
					t.Errorf("fuzz group target %s is in none of %v", tg, g.pkgs)
					continue
				}
				run = append(run, found)
			}
		}
		for _, k := range run {
			if seen[k] {
				t.Errorf("%s is in two fuzz groups", k)
			}
			seen[k] = true
		}
		covered += len(run)
		// Fuzz time, plus about 3 minutes to build each package's
		// instrumented test binary and 5 minutes of job setup.
		need := float64(len(run))*per.Minutes() + 3*float64(len(g.pkgs)) + 5
		if float64(timeout) < 1.3*need {
			t.Errorf("group %v: %d targets need about %.0f min; timeout-minutes %d leaves less than 30%% headroom", g.pkgs, len(run), need, timeout)
		}
	}
	var missing []string
	for p, ts := range targets {
		for _, tg := range ts {
			if !seen[p+" "+tg] {
				missing = append(missing, p+" "+tg)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("fuzz targets in no release fuzz group: %s", strings.Join(missing, "; "))
	}
	if covered != total {
		t.Errorf("the fuzz groups cover %d targets, the module has %d", covered, total)
	}
	cand := jobBlock(t, string(wf), "candidate")
	if !regexp.MustCompile(`needs: \[[^\]]*\bfuzz\b[^\]]*\]`).MatchString(cand) {
		t.Error("the candidate job doesn't need the whole fuzz matrix")
	}
}

// fuzz.sh runs a subset when given packages (one release matrix group):
// only their targets, and a group without a target fails.
func TestFuzzScriptRunsAPackageSubset(t *testing.T) {
	skipOnWindows(t)
	if testing.Short() {
		t.Skip("builds fuzz test binaries")
	}
	out, err := exec.Command("sh", "fuzz.sh", "1x", "./internal/keys/").CombinedOutput()
	if err != nil {
		t.Fatalf("fuzz.sh 1x ./internal/keys/: %v\n%s", err, out)
	}
	if s := string(out); !strings.Contains(s, "== herdr-dots/internal/keys Fuzz") || !strings.Contains(s, "fuzzed 1 targets for 1x each") ||
		strings.Contains(s, "internal/memory") {
		t.Fatalf("fuzz.sh ran more or less than the keys package's target:\n%s", s)
	}
	out, err = exec.Command("sh", "fuzz.sh", "1x", "./internal/version/").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "found 0 fuzz targets") {
		t.Fatalf("a group without fuzz targets must fail: %v\n%s", err, out)
	}
}
