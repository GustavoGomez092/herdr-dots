package transcript

import (
	"strings"
	"testing"
)

// T5 (post-rc hardening): a payload field of an unexpected JSON type (an
// object where a string was expected) drops only that field, not the whole
// record.
func TestParseCodexToleratesUnexpectedFieldTypes(t *testing.T) {
	src := strings.Join([]string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"Fix the API","kind":{"plain":true}}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"apply_patch","input":{"raw":true},"arguments":"*** Begin Patch\n*** Update File: api/users.go\n*** End Patch"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":{"text":"structured"}}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"Patched users.go"}}`,
	}, "\n")
	turns, err := ParseCodex(strings.NewReader(src))
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns %+v %v", turns, err)
	}
	if p := turns[0].TouchedPaths(); len(p) != 1 || p[0] != "api/users.go" {
		t.Fatalf("the tool call was dropped: %+v", turns[0])
	}
	if turns[0].Assistant != "Patched users.go" {
		t.Fatalf("assistant %q", turns[0].Assistant)
	}
}
