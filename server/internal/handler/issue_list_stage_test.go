package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DB-aware companion tests for #5235. package handler's TestMain skips the
// entire package when Postgres is unreachable (os.Exit(0)), so pure SQL
// regression coverage lives in the sibling package:
//
//	go test ./internal/handler/liststage/ -v
//
// These tests exercise the real unexported mapper issueListRowToResponse and
// only run when a database is available (TestMain successfully connected).

func TestIssueListRowToResponse_PropagatesStage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available (handler.TestMain skipped package setup)")
	}
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
	if testHandler == nil {
		t.Skip("database not available (handler.TestMain skipped package setup)")
	}
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
