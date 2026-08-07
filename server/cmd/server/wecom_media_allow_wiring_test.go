package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// routerSourceFile is parsed by TestWecomMediaAllowListIsWiredAtBoot. Kept as
// a constant so a rename shows up as one failing test rather than a silently
// skipped guard.
const routerSourceFile = "router.go"

const wecomMediaAllowEnv = "MULTICA_WECOM_MEDIA_ALLOW_CIDRS"

// TestWecomMediaAllowListIsWiredAtBoot guards the operator switch behind the
// media guard's allow-list.
//
// wecom.SetMediaAllowedPrefixes widens the SSRF guard that stands between a
// WeCom-supplied URL and the deployment's own network. It shipped once with no
// production caller at all: the exemption existed, its own doc comment claimed
// it was "called at boot from MULTICA_WECOM_MEDIA_ALLOW_CIDRS", and nothing
// read that variable. An operator behind a fake-IP proxy — where every
// hostname resolves into 198.18.0.0/15 and the guard therefore refuses every
// download — had the exemption and no way to turn it on.
//
// This parses router.go rather than asserting on behaviour, for the same
// reason TestMainUsesRouterOwnedBackgroundServices does: the regression being
// guarded is the absence of a call, and no value-level assertion can observe a
// call that was never made. The allow-list itself lives in unexported package
// state, so the only thing reachable from here is the wiring.
func TestWecomMediaAllowListIsWiredAtBoot(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, routerSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", routerSourceFile, err)
	}

	var (
		readsEnv  string // position of os.Getenv(wecomMediaAllowEnv)
		appliesIt string // position of wecom.SetMediaAllowedPrefixes(...)
	)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Both must sit in the SAME function. Reading the variable in one
		// place and calling the setter in another is how the two halves drift
		// apart again — e.g. a caller that only ever passes a constant.
		var envHere, applyHere string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch calleeName(call) {
			case "os.Getenv":
				if len(call.Args) == 1 && stringLit(call.Args[0]) == wecomMediaAllowEnv {
					envHere = fset.Position(call.Pos()).String()
				}
			case "wecom.SetMediaAllowedPrefixes":
				applyHere = fset.Position(call.Pos()).String()
			}
			return true
		})
		if envHere != "" && applyHere != "" {
			readsEnv, appliesIt = envHere, applyHere
			break
		}
		if envHere != "" {
			readsEnv = envHere
		}
		if applyHere != "" {
			appliesIt = applyHere
		}
	}

	if readsEnv == "" {
		t.Errorf("no os.Getenv(%q) in %s — the media guard's allow-list has no operator switch again",
			wecomMediaAllowEnv, routerSourceFile)
	}
	if appliesIt == "" {
		t.Errorf("no call to wecom.SetMediaAllowedPrefixes in %s — %s is read but never reaches the guard",
			routerSourceFile, wecomMediaAllowEnv)
	}
	if t.Failed() {
		t.FailNow()
	}

	// The errors SetMediaAllowedPrefixes returns are the only report an
	// operator gets for a typo'd CIDR. Dropping them on the floor turns a
	// malformed entry into a guard that is silently narrower than the .env
	// says it is.
	if !reportsMalformedCIDRs(fset, file) {
		t.Errorf("the []error from wecom.SetMediaAllowedPrefixes at %s is discarded — a malformed %s entry would be dropped silently",
			appliesIt, wecomMediaAllowEnv)
	}
}

// reportsMalformedCIDRs reports whether the SetMediaAllowedPrefixes call site
// consumes the returned errors — ranged over, assigned, or passed on — rather
// than being used as a bare statement.
func reportsMalformedCIDRs(fset *token.FileSet, file *ast.File) bool {
	var consumed bool
	ast.Inspect(file, func(n ast.Node) bool {
		if consumed {
			return false
		}
		switch stmt := n.(type) {
		case *ast.ExprStmt:
			// A bare `wecom.SetMediaAllowedPrefixes(...)` throws the errors away.
			if call, ok := stmt.X.(*ast.CallExpr); ok && calleeName(call) == "wecom.SetMediaAllowedPrefixes" {
				return false
			}
		case *ast.RangeStmt:
			if call, ok := stmt.X.(*ast.CallExpr); ok && calleeName(call) == "wecom.SetMediaAllowedPrefixes" {
				consumed = true
				return false
			}
		case *ast.AssignStmt:
			for _, rhs := range stmt.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok && calleeName(call) == "wecom.SetMediaAllowedPrefixes" {
					consumed = true
					return false
				}
			}
		}
		return true
	})
	return consumed
}

// stringLit returns the value of a plain string literal, or "".
func stringLit(e ast.Expr) string {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || len(lit.Value) < 2 {
		return ""
	}
	return lit.Value[1 : len(lit.Value)-1]
}
