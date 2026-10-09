package backend

import (
	"strings"
	"testing"

	"herdr-dots/internal/transcript"
)

func TestSystemPromptContainsPurposeAndContract(t *testing.T) {
	s := SystemPrompt(Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "terse"})
	for _, want := range []string{"Security", "Find auth bugs.", `"relevant"`, "relevant\": false", "1-2 sentences"} {
		if !strings.Contains(s, want) {
			t.Errorf("system prompt missing %q:\n%s", want, s)
		}
	}
	forced := SystemPrompt(Request{DotName: "Security", Purpose: "p", Verbosity: "normal", Force: true})
	if !strings.Contains(forced, "summoned") {
		t.Errorf("forced prompt must say the dot was summoned:\n%s", forced)
	}
}

func TestUserPromptIncludesTurnsAndQuestion(t *testing.T) {
	u := UserPrompt(Request{Turns: []transcript.Turn{{Index: 3, User: "add login", Assistant: "done"}}, Question: "is the cookie safe?"})
	for _, want := range []string{"### Turn 3", "add login", "QUESTION FROM THE DEVELOPER", "is the cookie safe?"} {
		if !strings.Contains(u, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
}
