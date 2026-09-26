package wecom

// language_lint_test.go — the copy rule, enforced instead of documented.
//
// strings.go already says "everything the adapter can say is a field here".
// That sentence stops nothing: a literal typed into the file that sends it
// compiles exactly as well as a pack lookup, reads fine to whoever wrote it,
// and pins that one surface to one language while every other surface follows
// the reader.
//
// This is the state the package was already in once — the binding prompt, the
// offline notices, the inbox card and the attachment notices each lived in the
// file that sent them — and nothing except this test would notice it coming
// back.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// packageGoFiles lists this package's non-test .go files, minus the ones named.
func packageGoFiles(t *testing.T, except ...string) []string {
	t.Helper()
	skip := map[string]bool{}
	for _, name := range except {
		skip[name] = true
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if skip[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// TestOnlyStringsGoHoldsUserVisibleCopy — a Chinese string literal anywhere but
// strings.go is copy that cannot be translated, because nothing outside the
// pack has a second language to offer. It compiles, it reads fine to whoever
// wrote it, and it silently pins one surface to one language while the rest of
// the adapter follows the reader — which is exactly the state this package was
// in when a colleague reading English got an English bubble and a Chinese
// everything-else.
//
// Comments are exempt by construction: this walks the AST and only ever looks
// at string literals, so the reasoning in a comment can still be written in
// whichever language explains it best.
func TestOnlyStringsGoHoldsUserVisibleCopy(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	type offence struct {
		file string
		line int
		text string
	}
	var offenders []offence

	// The two files below still hold their copy as literals. They are listed
	// with an exact count rather than left out of the walk, so the lint still
	// fails on a literal added anywhere else — including a new one in either of
	// these two — and fails again if one of these loses a literal without this
	// list being updated.
	pending := map[string]int{
		"wecom_channel.go": 1,
		"media_ingest.go":  2,
	}

	for _, name := range packageGoFiles(t, "strings.go") {
		f, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if !hasHan(lit.Value) {
				return true
			}
			offenders = append(offenders, offence{name, fset.Position(lit.Pos()).Line, lit.Value})
			return true
		})
	}

	for _, o := range offenders {
		if n := pending[filepath.Base(o.file)]; n > 0 {
			pending[filepath.Base(o.file)] = n - 1
			continue
		}
		t.Errorf("%s:%d holds user-visible copy as a literal: %s\n"+
			"Add a field to copyPack in strings.go, give it both languages, and read it through "+
			"copyFor(localeFor(...)). A literal here is a surface that cannot answer an English reader.",
			o.file, o.line, o.text)
	}
	// A count that did not run out means that file lost a literal without this
	// list being updated — the follow-up landed, and the allowance outlived it.
	for file, left := range pending {
		if left > 0 {
			t.Errorf("%s has %d fewer literals than this list allows for; "+
				"drop its entry now that its copy has moved", file, left)
		}
	}
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
