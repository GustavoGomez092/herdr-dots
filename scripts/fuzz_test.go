package scripts

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	m = regexp.MustCompile(`sh scripts/fuzz\.sh (\S+) \$\{\{ matrix\.packages \}\}`).FindStringSubmatch(job)
	if m == nil {
		t.Fatal("the fuzz job doesn't run sh scripts/fuzz.sh <time> ${{ matrix.packages }}")
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
	groups := regexp.MustCompile(`(?m)^\s+packages: (.+)$`).FindAllStringSubmatch(job, -1)
	if len(groups) < 4 {
		t.Fatalf("the fuzz matrix has %d groups, want at least 4", len(groups))
	}
	seen := map[string]bool{}
	covered := 0
	for _, g := range groups {
		pkgs := strings.Fields(g[1])
		n := 0
		for _, p := range pkgs {
			if seen[p] {
				t.Errorf("%s is in two fuzz groups", p)
			}
			seen[p] = true
			if len(targets[p]) == 0 {
				t.Errorf("fuzz group package %s has no fuzz target", p)
			}
			n += len(targets[p])
		}
		covered += n
		// Fuzz time, plus about 3 minutes to build each package's
		// instrumented test binary and 5 minutes of job setup.
		need := float64(n)*per.Minutes() + 3*float64(len(pkgs)) + 5
		if float64(timeout) < 1.3*need {
			t.Errorf("group %q: %d targets need about %.0f min; timeout-minutes %d leaves less than 30%% headroom", g[1], n, need, timeout)
		}
	}
	var missing []string
	for p, ts := range targets {
		if !seen[p] {
			missing = append(missing, p+" ("+strings.Join(ts, ", ")+")")
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
