package execenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestWindowsCodexHomeLinkDoesNotLaunchShell(t *testing.T) {
	t.Parallel()

	const path = "codex_home_link_windows.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, spec := range file.Imports {
		name, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("decode import %s: %v", spec.Path.Value, err)
		}
		if name == "os/exec" {
			t.Fatalf("%s must not import os/exec", path)
		}
	}

	var createDirLink *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "createDirLink" {
			createDirLink = function
			break
		}
	}
	if createDirLink == nil {
		t.Fatalf("%s does not declare createDirLink", path)
	}

	ast.Inspect(createDirLink.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == "os" && selector.Sel.Name == "Symlink" {
			t.Errorf("createDirLink must not expose owner directories through os.Symlink")
		}
		return true
	})

	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		if value == "cmd" || value == "cmd.exe" || value == "mklink" {
			t.Errorf("%s contains forbidden shell token %q", path, value)
		}
		return true
	})
}
