package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// TestDefaultAgentCommandNamesCoversAllProbes guards the invariant documented
// on defaultAgentCommandNames: the shell-fallback resolver only pre-fetches
// canonical paths for the bare command names in that list, so every agent the
// LoadConfig probe loop tries must appear there. A GUI/Launchpad-started
// daemon does not inherit the interactive shell PATH, so an agent missing from
// this list is undetectable when its binary lives only on the login-shell PATH
// (e.g. an `npm install -g` global). This test parses config.go's probe(...)
// calls so a new probe can't silently diverge from the fallback list.
func TestDefaultAgentCommandNamesCoversAllProbes(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "config.go", nil, 0)
	if err != nil {
		t.Fatalf("parse config.go: %v", err)
	}

	known := make(map[string]bool, len(defaultAgentCommandNames))
	for _, name := range defaultAgentCommandNames {
		known[name] = true
	}

	var missing []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "probe" {
			return true
		}
		// probe(envPathVar, commandName, envModelVar): the command name is the
		// second argument and is the value pre-fetched by the shell fallback.
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name := lit.Value
		if len(name) >= 2 {
			name = name[1 : len(name)-1] // strip surrounding quotes
		}
		if !known[name] {
			missing = append(missing, name)
		}
		return true
	})

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("probe() command names missing from defaultAgentCommandNames: %v; "+
			"add them so GUI-launched daemons can resolve these agents via the login shell", missing)
	}
}
