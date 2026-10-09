package ollama

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// Adapter conformance (Phase 4 final review item 5): the Phase 2 memory
// contract and the shared run rules, pinned per adapter. Ollama has no
// refusal signal and no prices: (e) pins that a failed run keeps its usage,
// (f) that any served model costs a known $0.

const learnReply = `{"relevant":true,"severity":"warn","message":"m","remember":[{"op":"add","id":"","scope":"project","kind":"convention","text":"Cookies are httpOnly.","paths":[]}]}`

func messages(body map[string]any) (system, user string) {
	for _, m := range body["messages"].([]any) {
		mm := m.(map[string]any)
		c, _ := mm["content"].(string)
		switch mm["role"] {
		case "system":
			system += c
		case "user":
			user += c
		}
	}
	return system, user
}

func formatProps(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return body["format"].(map[string]any)["properties"].(map[string]any)
}

func TestConformance(t *testing.T) {
	t.Run("a memory only in the user message", func(t *testing.T) {
		f := &fake{replies: []string{chat(pass, "")}}
		b, _ := newBackend(t, f)
		r := req()
		r.Memory = "MEMORY-BLOCK-7f3a"
		if _, err := b.Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		sys, user := messages(f.bodies[0])
		if strings.Contains(sys, "MEMORY-BLOCK-7f3a") || !strings.Contains(user, "MEMORY-BLOCK-7f3a") {
			t.Fatalf("memory placement: system %q user %q", sys, user)
		}
	})

	t.Run("b memory pass sends no tools and the ops-only schema", func(t *testing.T) {
		f := &fake{replies: []string{chat(`{"remember":[]}`, "")}}
		b, _ := newBackend(t, f)
		r := req()
		r.Pass, r.RepoRead, r.Cwd = backend.PassMemory, true, t.TempDir()
		if _, err := b.Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.bodies[0]["tools"]; ok {
			t.Fatal("a memory pass must not offer tools")
		}
		props := formatProps(t, f.bodies[0])
		if _, ok := props["relevant"]; ok || props["remember"] == nil {
			t.Fatalf("format %v", props)
		}
	})

	t.Run("c learn asks for remember and returns the ops", func(t *testing.T) {
		f := &fake{replies: []string{chat(learnReply, "")}}
		b, _ := newBackend(t, f)
		r := req()
		r.Learn = true
		res, err := b.Run(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if formatProps(t, f.bodies[0])["remember"] == nil {
			t.Fatal("learn format must carry remember")
		}
		if len(res.Remember) != 1 || res.Remember[0].Text != "Cookies are httpOnly." {
			t.Fatalf("remember %+v", res.Remember)
		}
	})

	for name, cwd := range map[string]string{"root": "/", "empty": ""} {
		t.Run("d no-tools note when tools are unavailable ("+name+" cwd)", func(t *testing.T) {
			f := &fake{replies: []string{chat(pass, "")}}
			b, _ := newBackend(t, f)
			r := req()
			r.RepoRead, r.Cwd = true, cwd
			if _, err := b.Run(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if sys, _ := messages(f.bodies[0]); !strings.Contains(sys, "unavailable for this pane") {
				t.Fatal("NoToolsNote missing")
			}
			if _, ok := f.bodies[0]["tools"]; ok {
				t.Fatal("no tools expected")
			}
		})
	}

	t.Run("e a failed run keeps its usage", func(t *testing.T) {
		cut := fmt.Sprintf(`{"model":"qwen3-coder:30b","message":{"role":"assistant","content":"{\"rel"},"done":true,"done_reason":"length","prompt_eval_count":%d,"eval_count":%d}`, 500, 64)
		f := &fake{replies: []string{cut}}
		b, _ := newBackend(t, f)
		res, err := b.Run(context.Background(), req())
		if err == nil {
			t.Fatal("a truncated reply is a run error")
		}
		if res.InputTokens != 500 || res.OutputTokens != 64 || !res.CostKnown || res.CostUSD != 0 {
			t.Fatalf("partial usage lost: %+v", res)
		}
	})

	t.Run("f any served model costs a known zero", func(t *testing.T) {
		f := &fake{replies: []string{`{"model":"qwen3-coder:30b-2026-08-01","message":{"role":"assistant","content":` + fmt.Sprintf("%q", pass) + `},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":2}`}}
		b, _ := newBackend(t, f)
		res, err := b.Run(context.Background(), req())
		if err != nil {
			t.Fatal(err)
		}
		if !res.CostKnown || res.CostUSD != 0 {
			t.Fatalf("%+v", res)
		}
	})
}
