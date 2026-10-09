package backend

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Every CLI backend's Run that builds a Cmd must close it (Cmd.Close runs
// Cleanup, which removes run files such as a long system prompt), whether or
// not BuildCmd sets Cleanup today.
func TestEveryRunThatBuildsACmdClosesIt(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Run" || fn.Body == nil {
				continue
			}
			builds, closes := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "BuildCmd" {
						builds = true
					}
				case *ast.DeferStmt:
					if sel, ok := n.Call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" {
						closes = true
					}
				}
				return true
			})
			if !builds {
				continue
			}
			checked++
			if !closes {
				t.Errorf("%s: %s.Run builds a Cmd without defer cmd.Close()", fset.Position(fn.Pos()), recvName(fn))
			}
		}
	}
	if checked < 2 {
		t.Fatalf("checked %d Run methods, want the claude and codex CLIs at least", checked)
	}
}

func recvName(fn *ast.FuncDecl) string {
	switch r := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return r.Name
	case *ast.StarExpr:
		if id, ok := r.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return "?"
}
