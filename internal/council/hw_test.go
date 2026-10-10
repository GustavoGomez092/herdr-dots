package council

import (
	"encoding/json"
	"strings"
	"testing"
)

// K2 (post-rc hardening): a preflight with no members publishes an empty
// list, never null, in Plan and in Select.
func TestPreflightWithNoMembersIsAnEmptyList(t *testing.T) {
	for name, p := range map[string]Preflight{
		"plan":   Plan(PlanInput{Rounds: 1, PaneLeft: 10}),
		"select": Plan(PlanInput{Rounds: 1, PaneLeft: 10, Candidates: []Candidate{{Slug: "a", CallsLeft: 5}}}).Select([]string{"b"}),
	} {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"members":[]`) {
			t.Errorf("%s: %s", name, b)
		}
	}
}
