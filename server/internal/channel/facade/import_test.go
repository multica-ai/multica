package facade_test

// AST-level static check: the channel/facade package must not import the
// concrete persistence layer (pkg/db). Per DESIGN §3.2 the facade is the
// single-direction outlet from channel/* into Multica's existing services;
// pulling pkg/db here would let inbound/intent/binding accidentally reach
// generated sqlc types and bypass the service-level permission boundary.
//
// TestCase §2 TC-facade-1 mandates this: "facade 不直接访问 queries（用 ast
// 静态断言 facade 包未 import pkg/db）".

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const forbiddenImportPrefix = "github.com/multica-ai/multica/server/pkg/db"

func TestFacadePackage_DoesNotImportPkgDB(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob facade source files: %v", err)
	}

	var sources []string
	for _, m := range matches {
		// Don't audit test files — mocks are allowed to be wherever, and a
		// _test.go file is not part of the production package binary anyway.
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		sources = append(sources, m)
	}
	if len(sources) == 0 {
		t.Fatal("no facade source files found — Red phase expected to fail here once Green creates them")
	}

	fset := token.NewFileSet()
	for _, src := range sources {
		f, err := parser.ParseFile(fset, src, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", src, err)
			}
			if strings.HasPrefix(path, forbiddenImportPrefix) {
				t.Errorf("%s imports forbidden package %q (channel/facade must call services, not access generated DB queries directly — DESIGN §3.2)", src, path)
			}
		}
	}
}
