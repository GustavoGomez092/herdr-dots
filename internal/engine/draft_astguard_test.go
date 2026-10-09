package engine

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// writeToolViolations lists the places in f that could start a run with write
// tools: a backend.Request literal (keyed, positional, or with its type elided
// inside a slice, array or map literal) that sets or may set Write or Pass,
// an assignment to a .Write field, a .Pass assignment other than PassMemory or
// PassReview, and (outside package backend, whose adapters only read it) any
// use of PassWrite. Reads of r.Write and comparisons with PassWrite inside
// package backend are fine: that is how adapters honor a draft.
func writeToolViolations(fset *token.FileSet, f *ast.File) []string {
	var out []string
	inBackend := f.Name.Name == "backend" || f.Name.Name == "toolloop"
	report := func(n ast.Node, what string) {
		out = append(out, fset.Position(n.Pos()).String()+": "+what)
	}
	isRequest := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			return x.Sel.Name == "Request"
		case *ast.Ident:
			return x.Name == "Request"
		}
		return false
	}
	checkRequestLit := func(lit *ast.CompositeLit) {
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				report(lit, "positional backend.Request literal")
				return
			}
			if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "Write" || k.Name == "Pass") {
				report(lit, "backend.Request literal sets "+k.Name)
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if x.Name == "PassWrite" && !inBackend {
				report(x, "PassWrite")
			}
		case *ast.CompositeLit:
			if x.Type != nil && isRequest(x.Type) {
				checkRequestLit(x)
			}
			// []backend.Request{{Write: true}}, [2]Request{{...}}, map[k]Request{k: {...}}
			var elem ast.Expr
			switch t := x.Type.(type) {
			case *ast.ArrayType:
				elem = t.Elt
			case *ast.MapType:
				elem = t.Value
			}
			if star, ok := elem.(*ast.StarExpr); ok {
				elem = star.X
			}
			if elem != nil && isRequest(elem) {
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						el = kv.Value
					}
					if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
						checkRequestLit(inner)
					}
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				sel, ok := l.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				switch sel.Sel.Name {
				case "Write":
					report(x, "assignment to .Write")
				case "Pass":
					okRHS := false
					if len(x.Lhs) == len(x.Rhs) {
						if r, ok := x.Rhs[i].(*ast.SelectorExpr); ok && (r.Sel.Name == "PassMemory" || r.Sel.Name == "PassReview") {
							okRHS = true
						}
					}
					if !okRHS {
						report(x, ".Pass assigned something other than PassMemory/PassReview")
					}
				}
			}
		}
		return true
	})
	return out
}

func violationsIn(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return writeToolViolations(fset, f)
}

// The detector itself: every way of building a write request is caught.
func TestTheWriteToolGuardCatchesEveryWayToBuildAWriteRequest(t *testing.T) {
	bad := map[string]string{
		"keyed Write":   `package x; var _ = backend.Request{Write: true}`,
		"keyed Pass":    `package x; var _ = backend.Request{Pass: "write"}`,
		"elided slice":  `package x; var _ = []backend.Request{{Write: true}}`,
		"elided map":    `package x; var _ = map[string]backend.Request{"a": {Pass: p}}`,
		"elided ptr":    `package x; var _ = []*backend.Request{{Write: true}}`,
		"positional":    `package x; var _ = backend.Request{"a", "b"}`,
		"assign Write":  `package x; func f(r backend.Request) { r.Write = true }`,
		"assign Pass":   `package x; func f(r backend.Request) { r.Pass = backend.PassWrite }`,
		"assign Pass 2": `package x; func f(r backend.Request, s string) { r.Pass = s }`,
		"PassWrite":     `package x; var _ = backend.PassWrite`,
	}
	for name, src := range bad {
		if len(violationsIn(t, src)) == 0 {
			t.Errorf("%s: not caught: %s", name, src)
		}
	}
	good := map[string]string{
		"memory pass":   `package x; func f(r backend.Request) { r.Pass, r.Question = backend.PassMemory, "" }`,
		"read":          `package x; func f(r backend.Request) bool { return r.Write }`,
		"other literal": `package x; var _ = view.LibraryDot{Write: true}`,
		"plain request": `package x; var _ = backend.Request{Model: "m", Cwd: "/"}`,
		"in backend":    `package backend; func WriteMode(r Request) bool { return r.Write && r.Pass == PassWrite }`,
	}
	for name, src := range good {
		if v := violationsIn(t, src); len(v) != 0 {
			t.Errorf("%s: wrongly flagged: %v", name, v)
		}
	}
}

// Write tools exist only in a draft run (reviews, councils, MCP asks, memory
// passes and summons stay read-only). This is the source half of the guard:
// no non-test file under internal/ or cmd/ but internal/engine/draft.go may
// build a write request. The run-time half is
// TestAsksCouncilsAndSummonsOfAWriteDotNeverWrite and
// TestOnlyTheDraftRunAsksForWriteTools.
func TestOnlyDraftsAskForWriteTools(t *testing.T) {
	fset := token.NewFileSet()
	checked := 0
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.ToSlash(path) == "../../internal/engine/draft.go" {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			checked++
			for _, v := range writeToolViolations(fset, f) {
				t.Errorf("%s (only internal/engine/draft.go may start a write run)", v)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 50 {
		t.Fatalf("only %d files checked: the walk is wrong", checked)
	}
}

// Run-time half: next to a draft, the attach review of the same write-capable
// dot is read-only, and exactly one run in all asked for write tools.
func TestOnlyTheDraftRunAsksForWriteTools(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	r.patchCard(t)
	r.draftsDone(t)
	r.dr.mu.Lock()
	defer r.dr.mu.Unlock()
	drafts, others := 0, 0
	for i, req := range r.dr.reqs {
		if strings.HasPrefix(req.Cwd, r.wts) { // a run in a draft worktree is the draft
			drafts++
			if !backend.WriteMode(req) || req.Learn || req.Force {
				t.Errorf("run %d: a draft sets Write and PassWrite together, with Learn and Force off: %+v", i, req)
			}
			continue
		}
		others++
		if req.Write || req.Pass == backend.PassWrite {
			t.Errorf("run %d is not a draft but asks for write tools: %+v", i, req)
		}
	}
	if drafts != 1 || others < 1 {
		t.Fatalf("%d draft runs and %d other runs, want one draft next to the review", drafts, others)
	}
}
