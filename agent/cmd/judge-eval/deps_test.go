//go:build judgeeval

package main

import (
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// judge-eval is no part of the agent (plan part 0): only -tags judgeeval
// builds it, nothing else carries the tag, and the agent's dependencies
// are the same with and without it.

// moduleRoot is the agent module, two folders up.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod in %s", root)
	}
	return root
}

func goTool(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("go not found: %v", err)
	}
	return p
}

func TestAgentDoesNotHoldJudgeEval(t *testing.T) {
	root, gobin := moduleRoot(t), goTool(t)
	list := func(args ...string) []string {
		cmd := exec.Command(gobin, append([]string{"list", "-deps"}, args...)...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %v: %v", args, err)
		}
		return strings.Fields(string(out))
	}
	plain := list("./cmd/asksemble-agent")
	tagged := list("-tags", "judgeeval", "./cmd/asksemble-agent")
	if !slices.Contains(plain, "github.com/asksemble/asksemble/agent/internal/core") {
		t.Fatalf("go list gave no agent packages: %d lines", len(plain))
	}
	for _, p := range plain {
		if strings.Contains(p, "cmd/judge-eval") || strings.Contains(p, "internal/fakecli") {
			t.Errorf("the agent depends on %s", p)
		}
	}
	if !slices.Equal(plain, tagged) {
		t.Errorf("-tags judgeeval changes the agent's dependencies:\n%v\n%v", plain, tagged)
	}
}

// buildTags are the tags a file's //go:build line names.
func buildTags(t *testing.T, path string) (tags []string, first bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			break
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		expr.Eval(func(tag string) bool {
			tags = append(tags, tag)
			return true
		})
		first = first || i == 0
	}
	return tags, first
}

func TestOnlyJudgeEvalIsTagged(t *testing.T) {
	root := moduleRoot(t)
	here, _ := filepath.Abs(".")
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		tags, first := buildTags(t, path)
		inTool := strings.EqualFold(filepath.Dir(path), here)
		switch {
		case inTool:
			n++
			if !first || !slices.Contains(tags, "judgeeval") {
				t.Errorf("%s does not start with //go:build judgeeval", path)
			}
			// Naming the tag is not enough: "judgeeval || linux" names it
			// too (S4 review SEC-2). The file must build nowhere without it.
			for _, goos := range []string{"windows", "linux", "darwin"} {
				bc := build.Default
				bc.GOOS, bc.BuildTags = goos, nil
				if ok, err := bc.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || ok {
					t.Errorf("%s builds without -tags judgeeval on %s (%v)", path, goos, err)
				}
			}
		case slices.Contains(tags, "judgeeval"):
			t.Errorf("%s outside cmd/judge-eval names judgeeval", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 20 {
		t.Errorf("only %d files in cmd/judge-eval", n)
	}
}

func TestCodexGetsSchemaDir(t *testing.T) {
	// Every codex.New here passes SchemaDir, so the judge schema never goes
	// to %APPDATA%\Asksemble, the folder of the agent that runs (R-30).
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil || len(names) == 0 {
		t.Fatalf("%d files, %v", len(names), err)
	}
	calls := 0
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "New" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "codex" {
				return true
			}
			calls++
			if !schemaDirSet(call) {
				t.Errorf("%s: codex.New without SchemaDir at %v", name, fset.Position(call.Pos()))
			}
			return true
		})
	}
	if calls == 0 {
		t.Error("no codex.New found")
	}
}

// schemaDirSet reports whether call's one argument is an Options literal
// that sets SchemaDir to something other than "".
func schemaDirSet(call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "SchemaDir" {
			continue
		}
		if bl, ok := kv.Value.(*ast.BasicLit); ok {
			s, _ := strconv.Unquote(bl.Value)
			return s != ""
		}
		return true
	}
	return false
}

// R-31: scripts\judge-eval.ps1 had param([Parameter(ValueFromRemainingArguments)]),
// which makes it an advanced script with PowerShell's common parameters,
// so "perturb -out FILE" stopped with "parameter name 'out' is ambiguous"
// (-OutVariable, -OutBuffer) before judge-eval ran. The script takes $args
// and stays ASCII (Windows PowerShell 5.1 reads it as the ANSI code page).
func TestScriptTakesEveryFlag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "..", "scripts", "judge-eval.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range b {
		if c > 0x7e || (c < 0x20 && c != '\n' && c != '\r' && c != '\t') {
			t.Fatalf("byte %d is %#x: keep the script ASCII", i, c)
		}
	}
	for n, line := range strings.Split(string(b), "\n") {
		code := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(code, "#") {
			continue
		}
		for _, bad := range []string{"param(", "param (", "[parameter(", "[cmdletbinding("} {
			if strings.Contains(code, bad) {
				t.Errorf("line %d has %q: an advanced script refuses -out as ambiguous", n+1, bad)
			}
		}
	}
	if !strings.Contains(string(b), "foreach ($a in $args)") {
		t.Error("the script no longer passes $args on")
	}
}
