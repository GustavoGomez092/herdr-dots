package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/pricing"
)

// Adapter conformance (Phase 4 final review item 5): the Phase 2 memory
// contract and the shared run rules, pinned per adapter.

const learnReply = `{"relevant":true,"severity":"warn","message":"m","remember":[{"op":"add","id":"","scope":"project","kind":"convention","text":"Cookies are httpOnly.","paths":[]}]}`

const passReply = `{"relevant":false,"severity":"info","message":""}`

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

func schemaProps(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	rf := body["response_format"].(map[string]any)
	return rf["json_schema"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
}

func TestConformance(t *testing.T) {
	t.Run("a memory only in the user message", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){chat("m", passReply, "", "0.001")}}
		r := req()
		r.Memory = "MEMORY-BLOCK-7f3a"
		if _, err := newBackend(t, f, nil).Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		sys, user := messages(f.bodies[0])
		if strings.Contains(sys, "MEMORY-BLOCK-7f3a") || !strings.Contains(user, "MEMORY-BLOCK-7f3a") {
			t.Fatalf("memory placement: system %q user %q", sys, user)
		}
	})

	t.Run("b memory pass sends no tools and the ops-only schema", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){chat("m", `{"remember":[]}`, "", "0.001")}}
		r := req()
		r.Pass, r.RepoRead, r.Cwd = backend.PassMemory, true, t.TempDir()
		if _, err := newBackend(t, f, nil).Run(context.Background(), r); err != nil {
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

	t.Run("b memory pass prompt-only fallback still parses ops", func(t *testing.T) {
		reject := func(w http.ResponseWriter) {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"response_format is not supported"}}`)
		}
		f := &fake{replies: []func(http.ResponseWriter){reject, chat("m", `{"remember":[]}`, "", "0.001")}}
		r := req()
		r.Pass = backend.PassMemory
		if _, err := newBackend(t, f, nil).Run(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.bodies[1]["tools"]; ok {
			t.Fatal("no tools on the fallback either")
		}
		if sys, _ := messages(f.bodies[1]); !strings.Contains(sys, "remember") {
			t.Fatal("the prompt must still describe the ops reply")
		}
	})

	t.Run("c learn asks for remember and returns the ops", func(t *testing.T) {
		f := &fake{replies: []func(http.ResponseWriter){chat("m", learnReply, "", "0.001")}}
		r := req()
		r.Learn = true
		res, err := newBackend(t, f, nil).Run(context.Background(), r)
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
			f := &fake{replies: []func(http.ResponseWriter){chat("m", passReply, "", "0.001")}}
			r := req()
			r.RepoRead, r.Cwd = true, cwd
			if _, err := newBackend(t, f, nil).Run(context.Background(), r); err != nil {
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

	t.Run("e refusal is a run error that keeps usage", func(t *testing.T) {
		refuse := func(w http.ResponseWriter) {
			fmt.Fprint(w, `{"id":"g","model":"m","choices":[{"message":{"role":"assistant","content":"","refusal":"no"},"finish_reason":"stop"}],"usage":{"prompt_tokens":300,"completion_tokens":4,"cost":0.002}}`)
		}
		f := &fake{replies: []func(http.ResponseWriter){refuse}}
		res, err := newBackend(t, f, nil).Run(context.Background(), req())
		if !errors.Is(err, backend.ErrRefused) {
			t.Fatalf("err = %v", err)
		}
		if res.InputTokens != 300 || res.OutputTokens != 4 || !res.CostKnown || res.CostUSD != 0.002 {
			t.Fatalf("partial usage lost: %+v", res)
		}
	})

	t.Run("f a dated served model is priced", func(t *testing.T) {
		// usage.cost is used as-is when present; without it the price table
		// is consulted, served id first, then undated, then requested.
		f := &fake{replies: []func(http.ResponseWriter){chat("qwen/qwen3-coder-2026-08-01", passReply, "", "")}}
		srv := newBackend(t, f, nil)
		b := New(backend.Options{Creds: srv.o.Creds, Provider: srv.o.Provider, Prices: pricing.Table{
			Prices: map[string]map[string]pricing.Price{"openrouter": {"qwen/qwen3-coder": {Input: 1, Output: 2}}},
		}})
		r := req()
		r.Model = "qwen/qwen3-coder"
		res, err := b.Run(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if !res.CostKnown || res.CostUSD <= 0 {
			t.Fatalf("%+v", res)
		}
	})
}
