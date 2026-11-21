package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInternalServerErrorsDoNotExposeCause keeps raw infrastructure errors on
// the server side. It walks complete call expressions, so multiline payloads
// and error variables with names other than err are covered too.
func TestInternalServerErrorsDoNotExposeCause(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read handler package: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !callUsesInternalServerError(call) {
				return true
			}
			ast.Inspect(call, func(child ast.Node) bool {
				causeCall, ok := child.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := causeCall.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "Error" && len(causeCall.Args) == 0 {
					pos := fset.Position(causeCall.Pos())
					t.Errorf("%s:%d returns a raw error from an internal-server-error path; send stable public copy and log the cause", name, pos.Line)
				}
				return true
			})
			return false
		})
	}
}

func callUsesInternalServerError(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		selector, ok := arg.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "StatusInternalServerError" {
			continue
		}
		pkg, ok := selector.X.(*ast.Ident)
		if ok && pkg.Name == "http" {
			return true
		}
	}
	return false
}
