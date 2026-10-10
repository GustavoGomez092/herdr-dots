package transcript

import (
	"strings"
	"testing"
)

// T1 (post-rc hardening): a compact summary and a built-in command's output
// echo (<local-command-stdout>/<local-command-stderr>) are not prompts, so
// they never become turns. The <command-name> record itself still does: only
// a later record tells a built-in command from a custom one the agent
// answers, and dropping a turn after the fact would shift the turn indexes
// the engine has already counted.
func TestParseClaudeSkipsCompactSummariesAndCommandOutput(t *testing.T) {
	sample := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"Add login"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Added."}]}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/compact</command-name>\n<command-message>compact</command-message>\n<command-args></command-args>"}}`,
		`{"type":"user","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"message":{"role":"user","content":"This session is being continued from a previous conversation that ran out of context."}}`,
		`{"type":"user","message":{"role":"user","content":"<local-command-stdout>Compacted (ctrl+o to see full summary)</local-command-stdout>"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"  <local-command-stderr>no cost yet</local-command-stderr>"}]}}`,
		`{"type":"user","message":{"role":"user","content":"<command-message>review is running…</command-message>\n<command-name>/review</command-name>"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Reviewed: two issues."}]}}`,
		`{"type":"user","message":{"role":"user","content":"Fix them"}}`,
	}, "\n")
	turns, err := ParseClaude(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, tr := range turns {
		users = append(users, tr.User)
	}
	if len(turns) != 4 || turns[0].User != "Add login" || !strings.Contains(turns[1].User, "/compact") ||
		!strings.Contains(turns[2].User, "/review") || turns[2].Assistant != "Reviewed: two issues." || turns[3].User != "Fix them" {
		t.Fatalf("turns %q", users)
	}
	for i, tr := range turns {
		if tr.Index != i+1 {
			t.Fatalf("turn %d has index %d", i+1, tr.Index)
		}
	}
}
