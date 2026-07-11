package liststage

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// The list handlers map pgtype.Int4 stage via int4ToPtr → util.Int4ToPtr.
// These tests exercise that real conversion on the shipped util path so a
// valid stage never becomes nil and an invalid stage stays nil — the
// behavior issueListRowToResponse relies on after the SQL fix lands a value
// in row.Stage.
func TestInt4ToPtr_ValidStage(t *testing.T) {
	got := util.Int4ToPtr(pgtype.Int4{Int32: 2, Valid: true})
	if got == nil {
		t.Fatal("expected non-nil pointer for Valid stage")
	}
	if *got != 2 {
		t.Fatalf("stage = %d, want 2", *got)
	}
}

func TestInt4ToPtr_InvalidStageIsNil(t *testing.T) {
	got := util.Int4ToPtr(pgtype.Int4{Valid: false})
	if got != nil {
		t.Fatalf("expected nil for invalid stage, got %v", *got)
	}
}
