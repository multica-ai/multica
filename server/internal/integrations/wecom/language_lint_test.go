package wecom

// language_lint_test.go — the two locale rules, enforced instead of documented.
//
// language.go already says "reach localeForSender through localeFor" and
// strings.go already says "everything the adapter can say is a field here".
// Neither sentence stops anything: Go has no file-level privacy, and a literal
// typed into the file that sends it compiles exactly as well as a pack lookup,
// reads fine to whoever wrote it, and pins that one surface to one language
// while every other surface follows the reader.
//
// This is the state the package was already in once — the copy for the
// greeting, the binding prompt, the inbox card and the bubble each lived in
// the file that sent it — and nothing except these two tests would notice it
// coming back.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
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

// TestOnlyLanguageGoResolvesASendersLocale — every user-visible string in this
// package is chosen by DESTINATION: a 1:1 reads the one person's profile
// language, a room reads the deployment's. localeFor is where that decision
// lives. localeForSender answers a different question — what does this PERSON
// read — and using it for a room is the bug this guards: a group message
// written in whichever member triggered it, in front of everyone else.
func TestOnlyLanguageGoResolvesASendersLocale(t *testing.T) {
	t.Parallel()

	var offenders []string
	for _, name := range packageGoFiles(t, "language.go") {
		body, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(body), "localeForSender(") {
			offenders = append(offenders, name)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("these files pick a locale from the SENDER rather than the destination: %v\n"+
			"Use localeFor(ctx, q, installationID, chatType, personID) instead — it answers "+
			"\"what does this destination read\", which is the only question the copy has. "+
			"A room is not a person: sending it one member's profile language is what this guards.",
			offenders)
	}
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

	// The two files below still hold their copy as literals, and each is the
	// subject of its own follow-up: wecom_channel.go's unsupported-kind receipt
	// is moved by the receipt-dedupe change, media_ingest.go's two notices by
	// the media-budget one. Listed rather than left out of the walk, so the
	// lint still fails on a literal added anywhere else — including a new one
	// in either of these two.
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

// TestEveryCopyPackFieldHasAReader — a copy field nothing renders is worse
// than a missing one. It reads as available: the next person adding a failure
// notice finds three ready-made fields, writes against them, and ships a
// notice nobody ever sees. Meanwhile every new locale has to translate lines
// that reach no screen, and the pin table in locale_wiring_test.go keeps them
// looking alive by asserting their wording.
//
// That is not hypothetical. TaskFailedNotice, TaskFailedAgentFallback and
// TaskFailedReason were rendered only by the DB outbound queue's
// outbox_sender.go. The queue was withdrawn, its renderer went with it, and
// the three fields stayed — with both locales filled in, pinned by a test, and
// zero readers. Nothing was red.
//
// A reader is a selector: cp.Foo, c.Progress.Bar, pack.InboxTypeLabels[k].
// Filling a field in (strings.go's two pack literals) is a WRITE and does not
// count — that is precisely the state this catches.
func TestEveryCopyPackFieldHasAReader(t *testing.T) {
	t.Parallel()

	read := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range packageGoFiles(t) {
		f, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				read[sel.Sel.Name] = true
			}
			return true
		})
	}

	var orphans []string
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, prefix string) {
		for i := range typ.NumField() {
			field := typ.Field(i)
			if field.Type.Kind() == reflect.Struct {
				walk(field.Type, prefix+field.Name+".")
				continue
			}
			if !read[field.Name] {
				orphans = append(orphans, prefix+field.Name)
			}
		}
	}
	walk(reflect.TypeOf(copyPack{}), "copyPack.")

	for _, name := range orphans {
		t.Errorf("%s is filled in for every locale and read by nothing outside the tests.\n"+
			"Either delete it (and its line in locale_wiring_test.go's pin table), or wire the "+
			"surface that says it. A field with no sender is a promise to the reader that no code keeps.",
			name)
	}
}
