package engine

import (
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The project dot run guard (carry-over 1 of Task 5's review): the engine and
// the CLI get a project dot to run ONLY through projectdots.Item.Runnable.
// Item.Dot and Item.Effective exist for trust previews and are filled in for
// untrusted files too, so no code outside internal/projectdots may take
// either as a dot value: only a read that ends in a plain field (a slug, a
// name, a model) is allowed. No code may read a dot's SourcePath either: the
// file is read once, by the scan, and the bytes that were hashed are the
// bytes that run.
//
// The guard type-checks the packages (go/types, sources only), so it knows
// which .Dot is an Item's and which is, say, a memory job's.

const itemType = "herdr-dots/internal/projectdots.Item"
const dotType = "herdr-dots/internal/dots.Dot"

// srcImporter type-checks herdr-dots packages from source with itself and
// the standard library with the source importer; third-party packages are
// empty stand-ins (nothing the guard looks at comes from them, and the
// checker's errors are ignored).
type srcImporter struct {
	fset *token.FileSet
	std  types.Importer
	root string // the module root
	pkgs map[string]*types.Package
}

func newSrcImporter(fset *token.FileSet, root string) *srcImporter {
	return &srcImporter{fset: fset, std: importer.ForCompiler(fset, "source", nil), root: root, pkgs: map[string]*types.Package{}}
}

func (l *srcImporter) Import(p string) (*types.Package, error) {
	if pk, ok := l.pkgs[p]; ok {
		return pk, nil
	}
	var pk *types.Package
	switch {
	case strings.HasPrefix(p, "herdr-dots/"):
		pk, _, _ = l.check(p, filepath.Join(l.root, filepath.FromSlash(strings.TrimPrefix(p, "herdr-dots/"))))
	case !strings.Contains(strings.SplitN(p, "/", 2)[0], "."):
		var err error
		if pk, err = l.std.Import(p); err != nil {
			return nil, err
		}
	default:
		pk = types.NewPackage(p, path.Base(p))
		pk.MarkComplete()
	}
	l.pkgs[p] = pk
	return pk, nil
}

// check type-checks the non-test files of dir for this platform.
func (l *srcImporter) check(p, dir string) (*types.Package, []*ast.File, *types.Info) {
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}, Types: map[ast.Expr]types.TypeAndValue{}}
	bp, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		pk := types.NewPackage(p, path.Base(p))
		pk.MarkComplete()
		return pk, nil, info
	}
	var files []*ast.File
	for _, n := range bp.GoFiles {
		if f, err := parser.ParseFile(l.fset, filepath.Join(dir, n), nil, 0); err == nil {
			files = append(files, f)
		}
	}
	return l.checkFiles(p, files, info), files, info
}

func (l *srcImporter) checkFiles(p string, files []*ast.File, info *types.Info) *types.Package {
	conf := types.Config{Importer: l, Error: func(error) {}}
	pk, _ := conf.Check(p, l.fset, files, info)
	return pk
}

func namedIs(t types.Type, full string) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	return n.Obj().Pkg().Path()+"."+n.Obj().Name() == full
}

// runGuardViolations lists the forbidden uses in files, and counts the
// allowed plain-field reads of an Item's Dot or Effective.
func runGuardViolations(fset *token.FileSet, files []*ast.File, info *types.Info) (bad []string, reads int) {
	for _, f := range files {
		parent := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				parent[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			s := info.Selections[sel]
			if s == nil || s.Kind() != types.FieldVal {
				return true
			}
			pos := fset.Position(sel.Pos()).String()
			name := sel.Sel.Name
			if name == "SourcePath" && namedIs(s.Recv(), dotType) {
				bad = append(bad, pos+": reads a dot's SourcePath")
				return true
			}
			if (name != "Dot" && name != "Effective") || !namedIs(s.Recv(), itemType) {
				return true
			}
			// Follow the field chain up: it.Effective.Backend.Kind is fine,
			// it.Effective (or it.Effective.Backend) as a value is not.
			var cur ast.Expr = sel
			for {
				p, ok := parent[cur].(*ast.SelectorExpr)
				if !ok || p.X != cur {
					break
				}
				ps := info.Selections[p]
				if ps == nil || ps.Kind() != types.FieldVal {
					break
				}
				cur = p
			}
			tv, ok := info.Types[cur]
			if _, basic := tv.Type.Underlying().(*types.Basic); ok && basic {
				reads++
				return true
			}
			bad = append(bad, pos+": uses a projectdots.Item's ."+name+" as a dot (run project dots only through Item.Runnable)")
			return true
		})
	}
	return bad, reads
}

var guardOnce struct {
	sync.Once
	fset  *token.FileSet
	imp   *srcImporter
	files map[string][]*ast.File
	infos map[string]*types.Info
}

// guardPackages type-checks the engine and the CLI once per test binary.
func guardPackages() (*token.FileSet, *srcImporter, map[string][]*ast.File, map[string]*types.Info) {
	g := &guardOnce
	g.Do(func() {
		g.fset = token.NewFileSet()
		g.imp = newSrcImporter(g.fset, "../..")
		g.files, g.infos = map[string][]*ast.File{}, map[string]*types.Info{}
		for p, dir := range map[string]string{"herdr-dots/internal/engine": ".", "herdr-dots/cmd/herdr-dots": "../../cmd/herdr-dots"} {
			_, files, info := g.imp.check(p, dir)
			g.files[p], g.infos[p] = files, info
		}
	})
	return g.fset, g.imp, g.files, g.infos
}

func TestProjectDotsRunOnlyThroughRunnable(t *testing.T) {
	fset, _, files, infos := guardPackages()
	for _, p := range []string{"herdr-dots/internal/engine", "herdr-dots/cmd/herdr-dots"} {
		if len(files[p]) < 5 {
			t.Fatalf("%s: only %d files checked: the loader is wrong", p, len(files[p]))
		}
		bad, reads := runGuardViolations(fset, files[p], infos[p])
		for _, b := range bad {
			t.Error(b)
		}
		if p == "herdr-dots/internal/engine" && reads == 0 {
			t.Error("the engine reads no Item field: the type information is missing, so the guard checks nothing")
		}
	}
}

// The detector itself.
func TestTheRunnableGuardCatchesEveryWayToRunAPreviewDot(t *testing.T) {
	fset, imp, _, _ := guardPackages()
	check := func(body string) []string {
		src := `package x
import (
	"os"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/projectdots"
)
var _ = os.ReadFile
type other struct{ Dot dots.Dot }
func run(dots.Dot) {}
func f(it projectdots.Item, s projectdots.Scan, d dots.Dot, o other, m map[string]dots.Dot) {
` + body + `
}`
		f, err := parser.ParseFile(fset, "snippet.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}, Types: map[ast.Expr]types.TypeAndValue{}}
		imp.checkFiles("x", []*ast.File{f}, info)
		bad, _ := runGuardViolations(fset, []*ast.File{f}, info)
		return bad
	}
	for name, body := range map[string]string{
		"run Effective":    `run(it.Effective)`,
		"run Dot":          `run(it.Dot)`,
		"merge Effective":  `m["x"] = it.Effective`,
		"copy Dot":         `x := it.Dot; run(x)`,
		"address":          `p := &it.Effective; run(*p)`,
		"indexed item":     `run(s.Items[0].Effective)`,
		"found item":       `i, _ := s.Find("a"); run(i.Dot)`,
		"struct sub-field": `b := it.Effective.Backend; _ = b`,
		"source path":      `_, _ = os.ReadFile(d.SourcePath)`,
		"item source path": `_ = it.Dot.SourcePath`,
	} {
		if len(check(body)) == 0 {
			t.Errorf("%s: not caught: %s", name, body)
		}
	}
	for name, body := range map[string]string{
		"runnable":     `if r := it.Runnable(); r != nil { run(*r) }`,
		"slug":         `_ = it.Dot.Slug`,
		"display":      `_, _ = it.Effective.Name, it.Effective.Backend.Kind`,
		"mode":         `_ = string(it.Effective.Delivery.Mode)`,
		"item fields":  `_, _, _ = it.Rel, it.Sum, it.Trusted`,
		"other dot":    `run(o.Dot)`,
		"user dot":     `run(d)`,
		"indexed slug": `_ = s.Items[0].Dot.Slug`,
	} {
		if v := check(body); len(v) != 0 {
			t.Errorf("%s: wrongly flagged: %v", name, v)
		}
	}
}
