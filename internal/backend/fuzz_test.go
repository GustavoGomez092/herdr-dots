package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// hasControl reports a control character other than newline and tab: model
// text must be sanitized before anything renders or stores it.
func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0
}

// seedReplies adds the recorded live replies (Phase 2 fixtures) to the corpus.
func seedReplies(f *testing.F) {
	files, _ := filepath.Glob("../memory/testdata/replies/*.txt")
	if len(files) == 0 {
		f.Log("no recorded replies under ../memory/testdata/replies: fuzzing from the inline seeds only")
	}
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	for _, s := range []string{
		`{"relevant":true,"severity":"warn","message":"Token logged."}`,
		"```json\n{\"relevant\":false}\n```",
		`{"relevant":true,"message":"x","remember":[{"op":"add","scope":"dot","kind":"note","text":"t"}]}`,
		`{"remember":null}`, `{"relevant":true,"message":"\u001b[2Jgone"}`, `}{`, ``,
	} {
		f.Add(s)
	}
}

func FuzzParseVerdict(f *testing.F) {
	seedReplies(f)
	f.Fuzz(func(t *testing.T, text string) {
		v, err := ParseVerdict(text)
		if err != nil {
			return
		}
		if v.Relevant && strings.TrimSpace(v.Message) == "" {
			t.Fatalf("relevant verdict with an empty message from %q", text)
		}
		if hasControl(v.Message) {
			t.Fatalf("unsanitized message %q", v.Message)
		}
		if v.Severity != "critical" && v.Severity != "warn" && v.Severity != "info" {
			t.Fatalf("severity %q", v.Severity)
		}
	})
}

func FuzzParseReply(f *testing.F) {
	seedReplies(f)
	f.Fuzz(func(t *testing.T, text string) {
		v, ops, err := ParseReply(text)
		if err != nil {
			if len(ops) != 0 {
				t.Fatal("ops returned with an error")
			}
			return
		}
		if v.Relevant && strings.TrimSpace(v.Message) == "" {
			t.Fatalf("relevant verdict with an empty message from %q", text)
		}
		if len(ops) > MaxReviewOps {
			t.Fatalf("%d ops, the cap is %d", len(ops), MaxReviewOps)
		}
		checkOps(t, ops)
	})
}

func FuzzParseMemoryOps(f *testing.F) {
	seedReplies(f)
	f.Fuzz(func(t *testing.T, text string) {
		ops, err := ParseMemoryOps(text)
		if err != nil {
			return
		}
		if len(ops) > MaxPassOps {
			t.Fatalf("%d ops, the cap is %d", len(ops), MaxPassOps)
		}
		checkOps(t, ops)
	})
}

func checkOps(t *testing.T, ops []MemoryOp) {
	t.Helper()
	for _, op := range ops {
		switch op.Op {
		case "add":
			if op.Text == "" {
				t.Fatalf("add without text: %+v", op)
			}
		case "update":
			if op.ID == "" || op.Text == "" {
				t.Fatalf("update without id or text: %+v", op)
			}
		case "forget":
			if op.ID == "" {
				t.Fatalf("forget without id: %+v", op)
			}
		default:
			t.Fatalf("op %q survived cleaning", op.Op)
		}
	}
}

// FuzzParseCLIOutput feeds arbitrary bytes to the parsers of the claude and
// codex CLIs' output (JSON with model text inside). An accepted review has a
// sanitized message and only clean memory ops.
func FuzzParseCLIOutput(f *testing.F) {
	f.Add([]byte(`{"is_error":false,"result":"{\"relevant\":true,\"severity\":\"warn\",\"message\":\"x\"}","total_cost_usd":0.01,"usage":{"input_tokens":3,"output_tokens":2}}`))
	f.Add([]byte(`{"is_error":true,"result":"\u001b[2Jboom","total_cost_usd":-1}`))
	f.Add([]byte(`{"type":"item.completed","item":{"type":"agent_message","text":"{\"relevant\":false}"}}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`))
	f.Add([]byte(`{"type":"turn.failed","error":{"message":"rate limited"}}`))
	f.Fuzz(func(t *testing.T, out []byte) {
		for name, parse := range map[string]func([]byte) (Result, error){"claude": ParseClaudeOutput, "codex": ParseCodexOutput} {
			res, err := parse(out)
			if err != nil {
				continue
			}
			if hasControl(res.Verdict.Message) {
				t.Fatalf("%s: unsanitized message %q", name, res.Verdict.Message)
			}
			if len(res.Remember) > MaxReviewOps {
				t.Fatalf("%s: %d ops, the cap is %d", name, len(res.Remember), MaxReviewOps)
			}
			checkOps(t, res.Remember)
		}
	})
}
