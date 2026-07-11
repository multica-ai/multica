package handler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression for #5235: the hand-written ListIssues / ListGroupedIssues SQL
// must SELECT and Scan stage. When stage was added (MUL-3508 / #4410) only the
// sqlc queries and DTO mappers were updated; the dynamic handlers were left
// with a dual source of truth and silently returned stage:null for every row.

func TestIssueListRowToResponse_PropagatesStage(t *testing.T) {
	row := db.ListIssuesRow{
		Title:  "staged child",
		Status: "todo",
		Stage:  pgtype.Int4{Int32: 2, Valid: true},
	}
	got := issueListRowToResponse(row, "MUL")
	if got.Stage == nil {
		t.Fatal("expected Stage to be set, got nil")
	}
	if *got.Stage != 2 {
		t.Fatalf("Stage = %d, want 2", *got.Stage)
	}
}

func TestIssueListRowToResponse_UnstagedIsNil(t *testing.T) {
	row := db.ListIssuesRow{
		Title:  "unstaged",
		Status: "todo",
		Stage:  pgtype.Int4{Valid: false},
	}
	got := issueListRowToResponse(row, "MUL")
	if got.Stage != nil {
		t.Fatalf("expected Stage nil for unstaged issue, got %v", *got.Stage)
	}
}

func TestHandwrittenIssueListSQLIncludesStage(t *testing.T) {
	// Pin the dual-source-of-truth surface: if someone rewrites the dynamic
	// SELECT lists without stage again, this fails before a production board
	// goes stage-blind.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "issue.go"))
	if err != nil {
		t.Fatalf("read issue.go: %v", err)
	}
	text := string(src)

	// ListIssues SELECT must end its issue column list with stage.
	if !strings.Contains(text, "i.number, i.project_id, i.metadata, i.stage") {
		t.Fatal("ListIssues hand-written SELECT is missing i.stage after i.metadata (#5235)")
	}
	// ListIssues Scan must include &row.Stage after &row.Metadata.
	if !strings.Contains(text, "&row.Metadata,\n\t\t\t&row.Stage,") &&
		!strings.Contains(text, "&row.Metadata,\n\t\t\t&row.Stage,\n\t\t)") {
		// Accept either Stage-before-close or Stage-before-GroupTotal patterns.
		if !strings.Contains(text, "&row.Metadata,\n\t\t\t&row.Stage") {
			t.Fatal("ListIssues / ListGroupedIssues Scan is missing &row.Stage after &row.Metadata (#5235)")
		}
	}
	// ListGroupedIssues outer SELECT must project stage before group_total.
	if !strings.Contains(text, "number, project_id, metadata, stage, group_total") {
		t.Fatal("ListGroupedIssues outer SELECT is missing stage before group_total (#5235)")
	}
	// Inner CTE SELECT must include i.stage.
	if !strings.Contains(text, "i.number, i.project_id, i.metadata, i.stage,") {
		t.Fatal("ListGroupedIssues CTE SELECT is missing i.stage (#5235)")
	}
}
