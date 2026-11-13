package main

// wecom_env_documented_test.go — every MULTICA_WECOM_* knob the boot block
// reads has to be reachable from a self-hosted deployment.
//
// Two files stand between an operator and one of these variables, and both are
// load-bearing. .env.example is where they find out the knob exists at all;
// there is no other list. docker-compose.selfhost.yml is what passes it into
// the backend container — exporting it on the host does nothing on its own,
// because compose only forwards what its `environment:` block names, so a
// variable missing there is set, ignored, and silent about it.
//
// MULTICA_WECOM_DEFAULT_LOCALE was missing from both. router.go has read it
// since the copy pack started following the reader's language, and a
// self-hosted deployment that set it on the host got the built-in zh-Hans with
// nothing saying why.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// routerSourceFile is the file the WeCom boot block lives in, relative to this
// package.
const routerSourceFile = "router.go"

// repoRoot is where .env.example and the compose files live, relative to this
// package (server/cmd/server). Go runs a test with its package directory as
// the working directory, so this is stable wherever the tree is checked out.
const repoRoot = "../../.."

var wecomEnvName = regexp.MustCompile(`^MULTICA_WECOM_[A-Z0-9_]+$`)

func TestEveryWecomEnvVarIsDocumentedAndPassedIntoTheContainer(t *testing.T) {
	t.Parallel()

	// The names as the code asks for them: string literals in router.go, not
	// words in a comment. secretbox.LoadKey("MULTICA_WECOM_SECRET_KEY") counts
	// the same as os.Getenv — what matters is that the process reads it, not
	// which helper does the reading.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, routerSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", routerSourceFile, err)
	}
	seen := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			if name := stringLit(arg); wecomEnvName.MatchString(name) {
				seen[name] = true
			}
		}
		return true
	})
	var read []string
	for name := range seen {
		read = append(read, name)
	}
	sort.Strings(read)

	if len(read) < 4 {
		t.Fatalf("found only %v in %s — the boot block reads more than that, so this test is no longer looking where the variables are",
			read, routerSourceFile)
	}

	envExample := readRepoFile(t, ".env.example")
	compose := readRepoFile(t, "docker-compose.selfhost.yml")

	for _, name := range read {
		// A settable line, not a mention in the comment block above it: an
		// operator copies .env.example to .env and fills values in, so a
		// variable that appears only in prose is one they have to know to add
		// by hand.
		if !hasLinePrefix(envExample, name+"=") {
			t.Errorf("router.go reads %s, but .env.example has no %s= line.\n"+
				"An operator has no other list of these; add the line and the paragraph above it that says what the value does.",
				name, name)
		}
		if !hasLinePrefix(compose, name+":") {
			t.Errorf("router.go reads %s, but the backend service in docker-compose.selfhost.yml does not pass it in.\n"+
				"Compose forwards only what its environment: block names, so setting %s on the host reaches the container as an empty string and the deployment silently runs on the built-in default.",
				name, name)
		}
	}
}

func readRepoFile(t *testing.T, name string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(filepath.Join(repoRoot, name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.Split(string(body), "\n")
}

// hasLinePrefix looks for a line that STARTS with prefix once its indentation
// is stripped — so a commented-out "# NAME=" does not count as documented and
// a YAML key nested under environment: does.
func hasLinePrefix(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

// stringLit is the value of a plain string literal, or "" for anything else.
func stringLit(e ast.Expr) string {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || len(lit.Value) < 2 {
		return ""
	}
	return lit.Value[1 : len(lit.Value)-1]
}
