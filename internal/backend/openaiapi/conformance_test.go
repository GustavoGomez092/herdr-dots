package openaiapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// Adapter conformance (Phase 4 final review item 5): the Phase 2 memory
// contract and the shared run rules, pinned per adapter.

const memBlock = "MEMORY-BLOCK-7f3a: cookies are httpOnly"

const learnReply = `{"relevant":true,"severity":"warn","message":"m","remember":[{"op":"add","id":"","scope":"project","kind":"convention","text":"Cookies are httpOnly.","paths":[]}]}`

func schemaProps(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	format := body["text"].(map[string]any)["format"].(map[string]any)
	return format["schema"].(map[string]any)["properties"].(map[string]any)
}

func userText(t *testing.T, body map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, it := range body["input"].([]any) {
		m := it.(map[string]any)
		if m["role"] == "user" {
			b.WriteString(toString(m["content"]))
		}
	}
	return b.String()
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, p := range x {
			if m, ok := p.(map[string]any); ok {
				b.WriteString(toString(m["text"]))
			}
		}
		return b.String()
	}
	return ""
}

func TestConformance(t *testing.T) {
	pass := textOut(`{"relevant":false,"severity":"info","message":""}`)

	t.Run("a memory only in the user message", func(t *testing.T) {
		f := &fake{replies: []string{response("gpt-6-sol", pass, 10, 0, 3)}}
		r := req()
		r.Memory = memBlock
		if _, err := newBackend(t, f).Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(f.bodies[0]["instructions"].(string), "MEMORY-BLOCK-7f3a") {
			t.Fatal("memory leaked into instructions")
		}
		if !strings.Contains(userText(t, f.bodies[0]), "MEMORY-BLOCK-7f3a") {
			t.Fatal("memory missing from the user message")
		}
	})

	t.Run("b memory pass sends no tools and the ops-only schema", func(t *testing.T) {
		f := &fake{replies: []string{response("gpt-6-sol", textOut(`{"remember":[]}`), 10, 0, 3)}}
		r := req()
		r.Pass, r.RepoRead, r.Cwd = backend.PassMemory, true, t.TempDir()
		if _, err := newBackend(t, f).Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.bodies[0]["tools"]; ok {
			t.Fatal("a memory pass must not offer tools")
		}
		props := schemaProps(t, f.bodies[0])
		if _, ok := props["relevant"]; ok || props["remember"] == nil {
			t.Fatalf("schema %v", props)
		}
	})

	t.Run("c learn asks for remember and returns the ops", func(t *testing.T) {
		f := &fake{replies: []string{response("gpt-6-sol", textOut(learnReply), 10, 0, 3)}}
		r := req()
		r.Learn = true
		res, err := newBackend(t, f).Run(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if schemaProps(t, f.bodies[0])["remember"] == nil {
			t.Fatal("learn schema must carry remember")
		}
		if len(res.Remember) != 1 || res.Remember[0].Text != "Cookies are httpOnly." {
			t.Fatalf("remember %+v", res.Remember)
		}
	})

	for name, cwd := range map[string]string{"root": "/", "empty": ""} {
		t.Run("d no-tools note when tools are unavailable ("+name+" cwd)", func(t *testing.T) {
			f := &fake{replies: []string{response("gpt-6-sol", pass, 10, 0, 3)}}
			r := req()
			r.RepoRead, r.Cwd = true, cwd
			if _, err := newBackend(t, f).Run(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.bodies[0]["instructions"].(string), "unavailable for this pane") {
				t.Fatal("NoToolsNote missing")
			}
			if _, ok := f.bodies[0]["tools"]; ok {
				t.Fatal("no tools expected")
			}
		})
	}

	t.Run("e refusal is a run error that keeps usage", func(t *testing.T) {
		out := `[{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"no"}]}]`
		f := &fake{replies: []string{response("gpt-6-sol", out, 100, 0, 7)}}
		res, err := newBackend(t, f).Run(context.Background(), req())
		if !errors.Is(err, backend.ErrRefused) {
			t.Fatalf("err = %v", err)
		}
		if res.InputTokens != 100 || res.OutputTokens != 7 || !res.CostKnown || res.CostUSD <= 0 {
			t.Fatalf("partial usage lost: %+v", res)
		}
	})

	t.Run("f a dated served model is priced", func(t *testing.T) {
		f := &fake{replies: []string{response("gpt-6-sol-2026-08-01", pass, 1000, 0, 100)}}
		res, err := newBackend(t, f).Run(context.Background(), req())
		if err != nil {
			t.Fatal(err)
		}
		if !res.CostKnown || res.CostUSD <= 0 || res.Model != "gpt-6-sol-2026-08-01" {
			t.Fatalf("%+v", res)
		}
	})
}
