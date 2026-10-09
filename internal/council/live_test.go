//go:build live

package council

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/transcript"
)

// Run with:
//   RECORD=1 go test -tags live ./internal/council/ -run LiveChair -v -timeout 40m
//   go test -tags live ./internal/council/ -run LiveCouncilWallClock -v -timeout 20m
// CHAIR_MODEL overrides the chair model (default sonnet).

type liveCouncil struct {
	question string
	turns    []transcript.Turn
	parts    []Part
}

func liveTurns(user, agent string, paths ...string) []transcript.Turn {
	return []transcript.Turn{{Index: 1, User: user, Assistant: agent, Tools: []transcript.ToolUse{{Name: "Edit", Paths: paths}}}}
}

func objecting(p Part, stance, reply, sev string) Part {
	p.Stance, p.Reply, p.ReplySeverity = stance, reply, sev
	return p
}

// liveCouncils are ten synthetic councils: 2-5 members, every severity, a
// failed member, duplicates to merge and round-2 stances.
func liveCouncils() []liveCouncil {
	auth := liveTurns("Refactor login to store the JWT and add a login form.",
		"Done. auth.ts now saves the token with localStorage.setItem('jwt', token); LoginForm.tsx posts to /api/login.", "src/auth.ts", "src/LoginForm.tsx")
	sql := liveTurns("Add a search endpoint for users by name.",
		"Added GET /users/search building the query with fmt.Sprintf(\"... WHERE name LIKE '%%%s%%'\", q).", "api/users.go")
	copyT := liveTurns("Write the hero headline and the signup button label.",
		"Headline: 'THE BEST PLATFORM FOR EVERYONE!!!'. Button: 'Click here to submit your registration information'.", "web/Hero.tsx")
	return []liveCouncil{
		{"Ready to merge the auth refactor?", auth, []Part{
			said("security", "Security", "critical", "The JWT is in localStorage, readable by any XSS payload. Use an httpOnly, Secure, SameSite cookie."),
			said("frontend", "Frontend", "warn", "LoginForm has no loading or error state, and the token sits in localStorage, which XSS can read."),
		}},
		{"", auth, []Part{
			said("security", "Security", "critical", "Token in localStorage is XSS-stealable."),
			said("frontend", "Frontend", "warn", "No disabled state on the submit button: double submits."),
			passed("copy", "Copy"),
			failed("backend", "Backend", "timed out"),
		}},
		{"Anything blocking?", sql, []Part{
			said("security", "Security", "critical", "SQL injection: the query interpolates q with Sprintf. Use a parameterized query."),
			said("backend", "Backend", "critical", "The search query is built with string formatting; switch to placeholders. Also add LIMIT."),
			said("perf", "Performance", "info", "LIKE '%q%' can't use an index; consider a trigram index if the table grows."),
		}},
		{"", sql, []Part{
			said("backend", "Backend", "warn", "No pagination on /users/search."),
			said("perf", "Performance", "warn", "Unbounded result set; add LIMIT and pagination."),
		}},
		{"Is the copy ready to ship?", copyT, []Part{
			said("copy", "Copy", "warn", "All caps and triple exclamation marks read as shouting; use sentence case."),
			said("a11y", "Accessibility", "info", "The button label is long; screen readers announce all of it. 'Sign up' is enough."),
			said("brand", "Brand", "info", "'Best platform for everyone' is generic; name the concrete benefit."),
		}},
		{"", copyT, []Part{
			said("copy", "Copy", "warn", "Headline is shouting."),
			passed("frontend", "Frontend"),
			failed("brand", "Brand", "error: claude failed: not logged in"),
		}},
		{"Ship it?", auth, []Part{
			objecting(said("security", "Security", "critical", "Move the JWT to an httpOnly cookie."), StanceAdd, "Cookies need CSRF protection: add a SameSite=strict flag or a CSRF token.", "warn"),
			objecting(said("frontend", "Frontend", "info", "Keep localStorage, it's simpler for the SPA."), StanceObject, "Security is right; simplicity doesn't outweigh token theft.", "info"),
		}},
		{"", sql, []Part{
			objecting(said("security", "Security", "critical", "Parameterize the search query."), StanceAgree, "Backend's LIMIT point is also needed to stop scraping.", "info"),
			objecting(said("backend", "Backend", "warn", "Add LIMIT 50 and a cursor."), StanceAgree, "Security's injection point is the blocker.", "info"),
			objecting(passed("perf", "Performance"), StanceAdd, "Log slow queries over 200ms so regressions show up.", "info"),
		}},
		{"What should I fix first?", auth, []Part{
			said("security", "Security", "critical", "Token storage (localStorage) first."),
			said("frontend", "Frontend", "warn", "Then the missing loading state."),
			said("copy", "Copy", "info", "Error message 'Error 401' should say 'Wrong email or password'."),
			said("backend", "Backend", "warn", "/api/login has no rate limit."),
			said("perf", "Performance", "info", "The login bundle pulls all of lodash; import only debounce."),
		}},
		{"", copyT, []Part{
			said("copy", "Copy", "warn", "Sentence case for the headline."),
			said("a11y", "Accessibility", "warn", "Contrast of the hero text on the gradient is about 2.8:1; WCAG AA needs 4.5:1."),
		}},
	}
}

func chairModel() string {
	if m := os.Getenv("CHAIR_MODEL"); m != "" {
		return m
	}
	return "sonnet"
}

// V1: does a real chair produce attributed digest sections inside the
// verdict JSON? With RECORD=1 every reply is saved for TestRecordedChairReplies.
func TestLiveChair(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not installed")
	}
	b, err := backend.New("claude-cli", backend.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	councils := liveCouncils()
	counts := map[string]int{}
	for i, c := range councils {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		res, err := b.Run(ctx, backend.Request{DotName: ChairName, Purpose: ChairPurpose, Verbosity: "normal",
			Model: chairModel(), Cwd: t.TempDir(), Turns: c.turns, Question: ChairQuestion(c.question, c.parts), Force: true})
		cancel()
		label := "malformed"
		if err == nil {
			if _, ok := ParseDigest(res.Verdict.Message); ok {
				label = "verified"
			} else {
				label = "unverified"
			}
		}
		counts[label]++
		t.Logf("council %02d: %s (err=%v)\n%s", i+1, label, err, res.Verdict.Message)
		if os.Getenv("RECORD") == "1" && res.Raw != "" {
			path := filepath.Join("testdata", "chair", fmt.Sprintf("%s-%02d.txt", label, i+1))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(res.Raw), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("V1 summary: %d verified, %d unverified, %d malformed of %d", counts["verified"], counts["unverified"], counts["malformed"], len(councils))
}

// V4: wall-clock time of a 5-member council at max_parallel_runs = 3.
func TestLiveCouncilWallClock(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not installed")
	}
	b, err := backend.New("claude-cli", backend.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	c := liveCouncils()[8] // the five-member council
	start := time.Now()
	sem := make(chan struct{}, 3)
	parts := make([]Part, len(c.parts))
	var wg sync.WaitGroup
	for i, p := range c.parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			t0 := time.Now()
			res, err := b.Run(ctx, backend.Request{DotName: p.Name, Purpose: "Review the conversation as the " + p.Name + " specialist.",
				Verbosity: "normal", Model: "sonnet", Cwd: t.TempDir(), Turns: c.turns, Question: c.question})
			parts[i] = Part{Slug: p.Slug, Name: p.Name, Verdict: res.Verdict}
			if err != nil {
				parts[i].Err = err.Error()
			}
			t.Logf("member %s: %s (err=%v)", p.Name, time.Since(t0).Round(time.Second), err)
		}()
	}
	wg.Wait()
	members := time.Since(start)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	t1 := time.Now()
	_, err = b.Run(ctx, backend.Request{DotName: ChairName, Purpose: ChairPurpose, Verbosity: "normal", Model: chairModel(),
		Cwd: t.TempDir(), Turns: c.turns, Question: ChairQuestion(c.question, parts), Force: true})
	t.Logf("V4 summary: members %s, chair %s (err=%v), total %s", members.Round(time.Second),
		time.Since(t1).Round(time.Second), err, time.Since(start).Round(time.Second))
}
