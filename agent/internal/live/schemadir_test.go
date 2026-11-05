package live

// This file has no build tag: it runs with every go test (test-all), and
// reads the live tests' sources without running them.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Every codex adapter of the live tests writes its schema files into a
// folder of the test (SchemaDir: t.TempDir()), never into the agent folder
// %APPDATA%\Asksemble of the agent the person runs, where codex.Options{}
// would put it (R-30).
func TestLiveCodexSchemaDirIsTheTests(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isSelector(call.Fun, "codex", "New") {
				return true
			}
			found++
			if !hasTempSchemaDir(call) {
				t.Errorf("%s: codex.New without SchemaDir: t.TempDir(); its schema files would go to the real agent folder", fset.Position(call.Pos()))
			}
			return true
		})
	}
	if found == 0 {
		t.Fatal("no codex.New in the live tests: this check reads nothing")
	}
}

// isSelector reports whether e is pkg.name.
func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// hasTempSchemaDir reports whether call is codex.New(codex.Options{...,
// SchemaDir: <x>.TempDir(), ...}).
func hasTempSchemaDir(call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok || !isSelector(lit.Type, "codex", "Options") {
		return false
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "SchemaDir" {
			continue
		}
		v, ok := kv.Value.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := v.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "TempDir" && len(v.Args) == 0
	}
	return false
}
