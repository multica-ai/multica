// Package liststage holds pure regression tests for the hand-written issue
// list SQL (#5235). It lives outside package handler so it is NOT gated by
// handler.TestMain, which os.Exit(0)s the entire package when Postgres is
// unreachable.
package liststage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func issueGoPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// liststage/ → handler/issue.go
	return filepath.Join(filepath.Dir(thisFile), "..", "issue.go")
}

func readIssueGo(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(issueGoPath(t))
	if err != nil {
		t.Fatalf("read issue.go: %v", err)
	}
	return string(src)
}

// containsInOrder reports whether needles appear in order (allowing any
// whitespace between them). Used so CRLF vs LF / tab width cannot flake.
func containsInOrder(text string, needles ...string) bool {
	pos := 0
	for _, n := range needles {
		i := strings.Index(text[pos:], n)
		if i < 0 {
			return false
		}
		pos += i + len(n)
	}
	return true
}

// TestListIssuesHandwrittenSQLSelectsAndScansStage is the primary regression
// for #5235: ListIssues dynamic SQL must include stage in both SELECT and Scan.
func TestListIssuesHandwrittenSQLSelectsAndScansStage(t *testing.T) {
	text := readIssueGo(t)

	if !strings.Contains(text, "i.number, i.project_id, i.metadata, i.stage") {
		t.Fatal("ListIssues hand-written SELECT is missing i.stage after i.metadata (#5235)")
	}
	if !containsInOrder(text, "&row.Metadata,", "&row.Stage,") &&
		!containsInOrder(text, "&row.Metadata,", "&row.Stage") {
		t.Fatal("ListIssues Scan is missing &row.Stage after &row.Metadata (#5235)")
	}
}

// TestListGroupedIssuesHandwrittenSQLSelectsAndScansStage covers the board
// endpoint, which had the same dual-source-of-truth bug as ListIssues.
func TestListGroupedIssuesHandwrittenSQLSelectsAndScansStage(t *testing.T) {
	text := readIssueGo(t)

	if !strings.Contains(text, "i.number, i.project_id, i.metadata, i.stage,") {
		t.Fatal("ListGroupedIssues CTE SELECT is missing i.stage (#5235)")
	}
	if !strings.Contains(text, "number, project_id, metadata, stage, group_total") {
		t.Fatal("ListGroupedIssues outer SELECT is missing stage before group_total (#5235)")
	}
	// Scan must pass Stage before GroupTotal.
	if !containsInOrder(text, "&row.Metadata,", "&row.Stage,", "&row.GroupTotal,") &&
		!containsInOrder(text, "&row.Metadata,", "&row.Stage,", "&row.GroupTotal") {
		t.Fatal("ListGroupedIssues Scan is missing &row.Stage before &row.GroupTotal (#5235)")
	}
}

// TestIssueListRowToResponseMapsStage ensures the DTO mapper (already correct
// before #5235) still wires Stage from ListIssuesRow. Guards against someone
// "fixing" the query but dropping the mapper field.
func TestIssueListRowToResponseMapsStage(t *testing.T) {
	text := readIssueGo(t)

	// Find issueListRowToResponse and require it sets Stage from i.Stage.
	const fn = "func issueListRowToResponse"
	idx := strings.Index(text, fn)
	if idx < 0 {
		t.Fatal("issueListRowToResponse not found in issue.go")
	}
	// Slice until the next top-level func (approximate).
	rest := text[idx:]
	end := strings.Index(rest[len(fn):], "\nfunc ")
	if end < 0 {
		end = len(rest) - len(fn)
	}
	body := rest[:len(fn)+end]
	if !strings.Contains(body, "Stage:") || !strings.Contains(body, "i.Stage") {
		t.Fatal("issueListRowToResponse must set Stage from i.Stage (#5235)")
	}
}
