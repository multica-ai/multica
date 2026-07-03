package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A tool_result row with populated call_id + is_error round-trips both fields
// onto the payload so the frontend can pair by id and render an error status
// (MUL-27).
func TestTaskMessageToPayloadCarriesCallIDAndIsError(t *testing.T) {
	m := db.TaskMessage{
		Seq:     7,
		Type:    "tool_result",
		Tool:    pgtype.Text{String: "bash", Valid: true},
		Output:  pgtype.Text{String: "boom", Valid: true},
		CallID:  pgtype.Text{String: "call-1", Valid: true},
		IsError: pgtype.Bool{Bool: true, Valid: true},
	}

	got := taskMessageToPayload(m, "task-1", "issue-1")

	if got.CallID != "call-1" {
		t.Fatalf("CallID = %q, want %q", got.CallID, "call-1")
	}
	if !got.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// Back-compat: a legacy row written before the migration carries NULL
// call_id / is_error (Valid=false). It must still serialize without panic and
// degrade to the zero value (empty call_id → positional pairing fallback,
// is_error false → not an error). (MUL-27)
func TestTaskMessageToPayloadLegacyNullColumns(t *testing.T) {
	m := db.TaskMessage{
		Seq:     3,
		Type:    "tool_result",
		Tool:    pgtype.Text{String: "read", Valid: true},
		Output:  pgtype.Text{String: "file contents", Valid: true},
		CallID:  pgtype.Text{Valid: false},
		IsError: pgtype.Bool{Valid: false},
	}

	got := taskMessageToPayload(m, "task-1", "issue-1")

	if got.CallID != "" {
		t.Fatalf("legacy CallID = %q, want empty", got.CallID)
	}
	if got.IsError {
		t.Fatalf("legacy IsError = true, want false")
	}
}
