package anthropicapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// Adapter conformance (Phase 4 final review item 5): the Phase 2 memory
// contract and the shared run rules, pinned per adapter.

const learnReply = `{"relevant":true,"severity":"warn","message":"m","remember":[{"op":"add","id":"","scope":"project","kind":"convention","text":"Cookies are httpOnly.","paths":[]}]}`

func systemText(body map[string]any) string {
	var b strings.Builder
	for _, s := range body["system"].([]any) {
		b.WriteString(s.(map[string]any)["text"].(string))
	}
	return b.String()
}

func userText(body map[string]any) string {
	var b strings.Builder
	for _, m := range body["messages"].([]any) {
		mm := m.(map[string]any)
		if mm["role"] != "user" {
			continue
		}
		for _, blk := range mm["content"].([]any) {
			if s, ok := blk.(map[string]any)["text"].(string); ok {
				b.WriteString(s)
			}
		}
	}
	return b.String()
}

func schemaProps(body map[string]any) map[string]any {
	oc := body["output_config"].(map[string]any)
	return oc["format"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
}

func TestConformance(t *testing.T) {
	pass := msg("claude-opus-5-5", "end_turn", text(verdictPass), 10, 5, 0, 0)

	t.Run("a memory only in the user message", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){pass}}
		b, _ := newBackend(t, f)
		r := req()
		r.Memory = "MEMORY-BLOCK-7f3a"
		if _, err := b.Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(systemText(f.bodies[0]), "MEMORY-BLOCK-7f3a") || !strings.Contains(userText(f.bodies[0]), "MEMORY-BLOCK-7f3a") {
			t.Fatal("memory must be in the user message only")
		}
	})

	t.Run("b memory pass sends no tools and the ops-only schema", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(`{"remember":[]}`), 10, 5, 0, 0)}}
		b, _ := newBackend(t, f)
		r := req()
		r.Pass, r.RepoRead, r.Cwd = backend.PassMemory, true, t.TempDir()
		if _, err := b.Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.bodies[0]["tools"]; ok {
			t.Fatal("a memory pass must not offer tools")
		}
		props := schemaProps(f.bodies[0])
		if _, ok := props["relevant"]; ok || props["remember"] == nil {
			t.Fatalf("schema %v", props)
		}
	})

	t.Run("c learn asks for remember and returns the ops", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(learnReply), 10, 5, 0, 0)}}
		b, _ := newBackend(t, f)
		r := req()
		r.Learn = true
		res, err := b.Run(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if schemaProps(f.bodies[0])["remember"] == nil {
			t.Fatal("learn schema must carry remember")
		}
		if len(res.Remember) != 1 || res.Remember[0].Text != "Cookies are httpOnly." {
			t.Fatalf("remember %+v", res.Remember)
		}
	})

	for name, cwd := range map[string]string{"root": "/", "empty": ""} {
		t.Run("d no-tools note when tools are unavailable ("+name+" cwd)", func(t *testing.T) {
			f := &fake{replies: []func(http.ResponseWriter){pass}}
			b, _ := newBackend(t, f)
			r := req()
			r.RepoRead, r.Cwd = true, cwd
			if _, err := b.Run(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(systemText(f.bodies[0]), "unavailable for this pane") {
				t.Fatal("NoToolsNote missing")
			}
			if _, ok := f.bodies[0]["tools"]; ok {
				t.Fatal("no tools expected")
			}
		})
	}

	t.Run("e refusal is a run error that keeps usage", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
			fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},"usage":{"input_tokens":120,"output_tokens":3}}`)
		}}}
		b, _ := newBackend(t, f)
		res, err := b.Run(context.Background(), req())
		if !errors.Is(err, backend.ErrRefused) {
			t.Fatalf("err = %v", err)
		}
		if res.InputTokens != 120 || res.OutputTokens != 3 || !res.CostKnown || res.CostUSD <= 0 {
			t.Fatalf("partial usage lost: %+v", res)
		}
	})

	t.Run("f a dated served model is priced", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){msg("claude-haiku-4-5-20251001", "end_turn", text(verdictPass), 1000, 100, 0, 0)}}
		b, _ := newBackend(t, f)
		r := req()
		r.Model = "claude-haiku-4-5"
		res, err := b.Run(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if want := (1000*1.0 + 100*5.0) / 1e6; !res.CostKnown || res.CostUSD != want {
			t.Fatalf("%+v want %v", res, want)
		}
	})
}
