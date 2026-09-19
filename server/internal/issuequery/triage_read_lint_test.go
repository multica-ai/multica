package issuequery_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The Triage read-path lint (MUL-7189 §2.4).
//
// Triage is excluded from work surfaces by one predicate repeated across many
// queries, which is the kind of rule that holds until someone writes query
// number thirty-four. There is no shared WHERE builder to hang it on: issue
// reads are a mix of sqlc files and three hand-built SQL strings. So the rule
// is enforced mechanically instead — every issue-set read either carries the
// predicate or says out loud, in the query, that it means to see everything.
//
// `-- triage: all` is that declaration. It is not a suppression: a reader of
// the query learns what the author decided about Triage, and a reviewer sees
// the decision in the diff that made it. Deleting a predicate without adding
// the marker fails here, which is the regression this lint exists to catch.
//
// What the lint does NOT do is decide which answer is right. Plenty of reads
// legitimately want every row — teardown, admin censuses, lock fences. It only
// insists that somebody answered.

const triageMarker = "-- triage: all"

var (
	// The base issue table only: the trailing \b does not match inside
	// `issue_to_label`, `issue_property` and the other side tables, because an
	// underscore is a word character.
	issueTableRef = regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+issue\b`)
	namedQuery    = regexp.MustCompile(`(?m)^-- name: (\S+) :(\w+)\s*$`)
	lineComment   = regexp.MustCompile(`(?m)--.*$`)
	topLevelCount = regexp.MustCompile(`(?im)^SELECT\s+count\s*\(`)
)

func repoDir(t *testing.T, parts ...string) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test's source file")
	}
	// internal/issuequery -> server
	root := filepath.Join(filepath.Dir(self), "..", "..")
	return filepath.Clean(filepath.Join(append([]string{root}, parts...)...))
}

type sqlQuery struct {
	file string
	name string
	kind string
	body string
}

// readsAnIssueSet reports whether a query returns a set of issues, or an
// aggregate over one. A read keyed to a single issue is not in scope: it
// answers a question about one row the caller already named, and Triage is
// about what a surface OFFERS, not about what a caller may look up.
//
// `:many` over the issue table is a set by definition. A `:one` is only a set
// read when its outermost projection is an aggregate — which is how `CountX`
// queries get caught and lock fences (`WITH locked_issue AS (...) SELECT
// comment.*`) stay out, even though their CTEs count rows.
func (q sqlQuery) readsAnIssueSet() bool {
	code := strings.TrimSpace(lineComment.ReplaceAllString(q.body, ""))
	fields := strings.Fields(code)
	if len(fields) == 0 {
		return false
	}
	if verb := strings.ToUpper(fields[0]); verb != "SELECT" && verb != "WITH" {
		return false // a write is not a read surface
	}
	if !issueTableRef.MatchString(code) {
		return false
	}
	switch q.kind {
	case "many":
		return true
	case "one":
		return topLevelCount.MatchString(code)
	default:
		return false
	}
}

func (q sqlQuery) answersTriage() bool {
	return strings.Contains(q.body, "triage_state") ||
		strings.Contains(q.body, triageMarker)
}

func loadNamedQueries(t *testing.T) []sqlQuery {
	t.Helper()
	dir := repoDir(t, "pkg", "db", "queries")
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no query files under %s — the lint is looking in the wrong place", dir)
	}

	var queries []sqlQuery
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(content)
		headers := namedQuery.FindAllStringSubmatchIndex(text, -1)
		for i, header := range headers {
			end := len(text)
			if i+1 < len(headers) {
				end = headers[i+1][0]
			}
			queries = append(queries, sqlQuery{
				file: filepath.Base(file),
				name: text[header[2]:header[3]],
				kind: text[header[4]:header[5]],
				body: text[header[1]:end],
			})
		}
	}
	return queries
}

// TestIssueSetQueriesAnswerTriage is the lint. Delete `AND i.triage_state IS
// NULL` from any list, board or count query and this is what fails.
func TestIssueSetQueriesAnswerTriage(t *testing.T) {
	var unanswered []string
	total := 0
	for _, q := range loadNamedQueries(t) {
		if !q.readsAnIssueSet() {
			continue
		}
		total++
		if !q.answersTriage() {
			unanswered = append(unanswered, q.file+" "+q.name+" :"+q.kind)
		}
	}
	if total == 0 {
		t.Fatal("the lint matched no issue-set queries at all — its scope rule has stopped working")
	}
	if len(unanswered) > 0 {
		sort.Strings(unanswered)
		t.Fatalf("%d issue-set queries say nothing about Triage.\n"+
			"Add `AND <alias>.triage_state IS NULL` if the query is a work surface,\n"+
			"or a `%s` comment with the reason if it means to read every row:\n  %s",
			len(unanswered), triageMarker, strings.Join(unanswered, "\n  "))
	}
}

// handlerIssueSQL is the registry of handler functions that build issue SQL by
// hand, with how each one gets its Triage scope.
//
// Three of them own a WHERE clause that every other query in this list is
// composed from, so the predicate is added once per builder rather than once
// per query string. That is exactly why a text lint cannot cover this side:
// `FROM issue i WHERE %s` says nothing about what `%s` contains. What the
// registry catches instead is a NEW hand-built issue read appearing in the
// handler package — it fails until its author comes here and records which
// scope it takes, which is the moment to notice it needs one.
//
// The behavior these reasons claim is asserted in the handler package's own
// tests; this map only makes sure no read escapes having a claim.
var handlerIssueSQL = map[string]string{
	"issue.go :: ListIssues":                            "builds its own WHERE; opens with issueTriageWhere(query, \"i\"). Its count query reuses the same clause.",
	"issue.go :: ListGroupedIssues":                     "builds its own WHERE; opens with issueTriageWhere(query, \"i\"). Group totals are a window over the same rows.",
	"issue.go :: buildSearchQuery":                      "the one widening surface: issuequery.WorkSurface unless include_triage asked for both (§2.4).",
	"issue_table_query.go :: compileIssueTableQuery":    "not in this list because it holds no FROM clause — it is the WHERE builder the four functions below interpolate.",
	"issue_table_rows.go :: ListIssueTableRows":         "interpolates compileIssueTableQuery's WHERE into every CTE and its total.",
	"issue_table_group.go :: ListIssueTableGroups":      "interpolates compileIssueTableQuery's WHERE into the grouped, empty-group and compound CTEs.",
	"issue_table_facets.go :: ListIssueTableFacets":     "interpolates compileIssueTableQuery's WHERE into the batch facet query.",
	"issue_table_facets.go :: issueTableFacetQuery":     "interpolates the compiled WHERE into each per-facet count.",
	"issue_table_facets.go :: issueTableBaseFacetQuery": "interpolates the compiled WHERE into the working-agents facet.",
	"issue_table_group.go :: resolveIssueTableGroup":    "reads ONE issue by id to label a group header. Not a set, and a group whose key is a Triage entry cannot be produced by a scoped query anyway.",
	"issue_table_group.go :: contextExpression":         "correlated lookup of a row's own parent for the group descriptor, not a set.",
	"issue_move.go :: issueMoveAnchorPosition":          "reads one issue's position by id. Position is a shared ordering space that Triage rows sit in too, so it must NOT be scoped.",
}

// TestHandlerIssueSQLIsRegistered fails when a handler function starts building
// issue SQL without recording how it handles Triage.
func TestHandlerIssueSQLIsRegistered(t *testing.T) {
	dir := repoDir(t, "internal", "handler")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no handler sources under %s — the lint is looking in the wrong place", dir)
	}

	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		base := filepath.Base(file)
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text := lit.Value
				if unquoted, err := strconv.Unquote(lit.Value); err == nil {
					text = unquoted
				}
				if issueTableRef.MatchString(text) {
					found[base+" :: "+fn.Name.Name] = true
				}
				return true
			})
		}
	}

	var unregistered, stale []string
	for key := range found {
		if _, ok := handlerIssueSQL[key]; !ok {
			unregistered = append(unregistered, key)
		}
	}
	for key := range handlerIssueSQL {
		// compileIssueTableQuery is registered to document the indirection even
		// though it holds no FROM clause of its own.
		if !found[key] && !strings.HasSuffix(key, "compileIssueTableQuery") {
			stale = append(stale, key)
		}
	}
	sort.Strings(unregistered)
	sort.Strings(stale)

	if len(unregistered) > 0 {
		t.Errorf("handler functions build issue SQL but are not in handlerIssueSQL.\n"+
			"Record which Triage scope each one takes — a work surface carries\n"+
			"issuequery.WorkSurface, anything else needs a reason:\n  %s",
			strings.Join(unregistered, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("handlerIssueSQL names functions that no longer build issue SQL; remove them:\n  %s",
			strings.Join(stale, "\n  "))
	}
}
