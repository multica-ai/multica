package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestPersonalAccessTokenRoutesUseHumanActorGuard parses the production router
// so the test fails if the guard is removed from, or moved outside, the token
// route group. The handler package test covers middleware behavior; this test
// covers the route-registration wiring that the handler package cannot import.
func TestPersonalAccessTokenRoutesUseHumanActorGuard(t *testing.T) {
	const sourceFile = "router.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", sourceFile, err)
	}

	guarded := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 || selectorMethod(call.Fun) != "Route" || tokenString(call.Args[0]) != "/api/tokens" {
			return true
		}
		routeBody, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return true
		}
		ast.Inspect(routeBody.Body, func(routeNode ast.Node) bool {
			useCall, ok := routeNode.(*ast.CallExpr)
			if !ok || selectorMethod(useCall.Fun) != "Use" || len(useCall.Args) != 1 {
				return true
			}
			guarded = selectorName(useCall.Args[0]) == "handler.RequireHumanActor"
			return !guarded
		})
		return !guarded
	})

	if !guarded {
		t.Fatalf("router.go must attach handler.RequireHumanActor inside r.Route(\"/api/tokens\", ...)")
	}
}

func selectorMethod(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

func tokenString(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || len(lit.Value) < 2 {
		return ""
	}
	return lit.Value[1 : len(lit.Value)-1]
}

func selectorName(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if ident, ok := sel.X.(*ast.Ident); ok {
		return ident.Name + "." + sel.Sel.Name
	}
	if nested, ok := sel.X.(*ast.SelectorExpr); ok {
		if ident, ok := nested.X.(*ast.Ident); ok {
			return ident.Name + "." + nested.Sel.Name + "." + sel.Sel.Name
		}
	}
	return ""
}
