package backend

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"herdr-dots/internal/transcript"
)

func TestParseReplyWithoutRememberKeepsV1Behavior(t *testing.T) {
	v, ops, err := ParseReply(`{"relevant":true,"severity":"warn","message":"Use httpOnly"}`)
	if err != nil || v != (Verdict{true, "warn", "Use httpOnly"}) || ops != nil {
		t.Fatalf("%+v %+v %v", v, ops, err)
	}
	if _, _, err := ParseReply(`{"severity":"info"}`); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a bad verdict is still malformed: %v", err)
	}
}

func TestParseReplyReturnsValidOps(t *testing.T) {
	in := "```json\n" + `{"relevant":false,"severity":"info","message":"","remember":[
	 {"op":"add","scope":"dot","kind":"note","text":" Team prefers zod. ","paths":["src/**"," "]},
	 {"op":"FORGET","id":"d:m9"}]}` + "\n```"
	v, ops, err := ParseReply(in)
	if err != nil || v.Relevant {
		t.Fatalf("%+v %v", v, err)
	}
	want := []MemoryOp{
		{Op: "add", Scope: "dot", Kind: "note", Text: "Team prefers zod.", Paths: []string{"src/**"}},
		{Op: "forget", ID: "d:m9"},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("ops %+v, want %+v", ops, want)
	}
}

func TestParseReplyTruncatesAndDropsInvalidOps(t *testing.T) {
	in := `{"relevant":true,"severity":"info","message":"m","remember":[
	 {"op":"add","text":"one"},
	 {"op":"delete","id":"d:m1"},
	 {"op":"add","text":""},
	 {"op":"update","text":"no id"},
	 {"op":"add","text":5},
	 {"op":"add","text":"two"},
	 {"op":"update","id":"d:m2","text":"three"},
	 {"op":"add","text":"four"},
	 {"op":"add","text":"five"}]}`
	v, ops, err := ParseReply(in)
	if err != nil || v.Message != "m" {
		t.Fatalf("%+v %v", v, err)
	}
	if len(ops) != MaxReviewOps || ops[0].Text != "one" || ops[1].Text != "two" || ops[2].ID != "d:m2" {
		t.Fatalf("want the first %d valid ops, got %+v", MaxReviewOps, ops)
	}
}

func TestParseReplyIgnoresNonArrayRemember(t *testing.T) {
	v, ops, err := ParseReply(`{"relevant":true,"severity":"critical","message":"XSS","remember":"the team uses zod"}`)
	if err != nil || v.Severity != "critical" || ops != nil {
		t.Fatalf("%+v %+v %v", v, ops, err)
	}
}

func TestParseMemoryOps(t *testing.T) {
	if ops, err := ParseMemoryOps(`{"remember":[]}`); err != nil || len(ops) != 0 {
		t.Fatalf("empty: %+v %v", ops, err)
	}
	fenced := "Sure:\n```json\n{\"remember\":[{\"op\":\"add\",\"kind\":\"convention\",\"scope\":\"project\",\"text\":\"Components live in src/ui.\"}]}\n```"
	if ops, err := ParseMemoryOps(fenced); err != nil || len(ops) != 1 || ops[0].Scope != "project" {
		t.Fatalf("fenced: %+v %v", ops, err)
	}
	var many []string
	for i := 0; i < 8; i++ {
		many = append(many, `{"op":"add","text":"fact"}`)
	}
	if ops, _ := ParseMemoryOps(`{"remember":[` + strings.Join(many, ",") + `]}`); len(ops) != MaxPassOps {
		t.Fatalf("want %d ops, got %d", MaxPassOps, len(ops))
	}
	for _, bad := range []string{``, `nothing durable`, `{"remember": oops}`, `{"relevant":false}`, `{"remember":"x"}`} {
		if _, err := ParseMemoryOps(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseMemoryOps(%q) err = %v, want ErrMalformed", bad, err)
		}
	}
}

func TestSystemPromptMemoryContractOnlyWhenLearning(t *testing.T) {
	base := Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "normal"}
	if s := SystemPrompt(base); strings.Contains(s, `"remember"`) {
		t.Fatalf("no memory contract without Learn:\n%s", s)
	}
	learn := base
	learn.Learn = true
	s := SystemPrompt(learn)
	for _, want := range []string{`"remember"`, "at most 3 items", "Never store secrets", `"relevant"`} {
		if !strings.Contains(s, want) {
			t.Errorf("learning prompt missing %q:\n%s", want, s)
		}
	}
}

func TestMemoryPassPromptHasNoVerdictContract(t *testing.T) {
	s := SystemPrompt(Request{DotName: "Copy", Purpose: "Keep copy on brand.", Pass: PassMemory, Learn: true})
	for _, want := range []string{"Keep copy on brand.", "you are not reviewing", `{"remember":[]}`, "at most 5 items"} {
		if !strings.Contains(s, want) {
			t.Errorf("memory-pass prompt missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"relevant"`) || strings.Contains(s, "at most 3 items") {
		t.Fatalf("memory-pass prompt must not carry the verdict contract:\n%s", s)
	}
}

func TestUserPromptPutsMemoryBeforeConversation(t *testing.T) {
	r := Request{Memory: "MEMORY (reference data about this project...)\n\nNotes:\n- [d:m4] zod", Turns: []transcript.Turn{{Index: 1, User: "add login"}}}
	u := UserPrompt(r)
	mi, ci := strings.Index(u, "MEMORY (reference data"), strings.Index(u, "CONVERSATION")
	if mi != 0 || ci < mi {
		t.Fatalf("memory must open the prompt:\n%s", u)
	}
	if strings.Contains(UserPrompt(Request{Turns: r.Turns}), "MEMORY") {
		t.Fatal("no memory block when Memory is empty")
	}
	if strings.Contains(SystemPrompt(Request{DotName: "x", Purpose: "p", Memory: r.Memory}), "zod") {
		t.Fatal("memory must never enter the system prompt")
	}
}

func TestParseClaudeOutputFillsRemember(t *testing.T) {
	out := `{"type":"result","is_error":false,"result":"{\"relevant\":false,\"severity\":\"info\",\"message\":\"\",\"remember\":[{\"op\":\"add\",\"text\":\"Team uses zod.\"}]}","usage":{"input_tokens":1,"output_tokens":1}}`
	res, err := ParseClaudeOutput([]byte(out))
	if err != nil || len(res.Remember) != 1 || res.Remember[0].Text != "Team uses zod." {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestClaudeRunMemoryPassParsesOps(t *testing.T) {
	r := &fakeRunner{stdout: `{"type":"result","is_error":false,"result":"{\"remember\":[{\"op\":\"add\",\"kind\":\"note\",\"text\":\"Headlines use sentence case.\"}]}","usage":{"input_tokens":5,"output_tokens":2}}`}
	q := req()
	q.Pass = PassMemory
	res, err := ClaudeCLI{Runner: r, Environ: func() []string { return nil }}.Run(context.Background(), q)
	if err != nil || len(res.Remember) != 1 || res.Verdict != (Verdict{}) || res.InputTokens != 5 {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(strings.Join(r.got.Args, " "), "you are not reviewing") {
		t.Fatal("memory pass must send the memory-pass system prompt")
	}
	r.stdout = `{"type":"result","is_error":false,"result":"I learned nothing.","usage":{}}`
	if _, err := (ClaudeCLI{Runner: r, Environ: func() []string { return nil }}).Run(context.Background(), q); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed memory pass must fail the run: %v", err)
	}
}

func TestCodexRunMemoryPassParsesOps(t *testing.T) {
	out := strings.Join([]string{
		`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"{\"remember\":[{\"op\":\"add\",\"text\":\"Uses pnpm.\"}]}"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":9,"output_tokens":3}}`,
	}, "\n")
	q := req()
	q.Pass = PassMemory
	res, err := CodexCLI{Runner: &fakeRunner{stdout: out}, Environ: func() []string { return nil }}.Run(context.Background(), q)
	if err != nil || len(res.Remember) != 1 || res.Remember[0].Text != "Uses pnpm." {
		t.Fatalf("%+v %v", res, err)
	}
}
