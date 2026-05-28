package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dagcore"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mockQueries is a minimal stub for testing DAGService logic without a DB.
type mockQueries struct {
	*db.Queries
}

func TestNewDAGService(t *testing.T) {
	svc := NewDAGService(nil, nil)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestDAGServiceAppendEventValidation(t *testing.T) {
	svc := NewDAGService(nil, nil)
	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	// Missing agent ID should fail validation before DB touch
	event := dagcore.Event{
		ID:        "evt-1",
		RecordIDs: []string{"rec-1"},
		Operation: dagcore.OperationCreate,
	}
	_, err := svc.AppendEvent(context.Background(), wsID, event)
	if err == nil {
		t.Fatal("expected validation error for missing agent_id")
	}
}

func TestDAGServiceDetectConflicts(t *testing.T) {
	svc := NewDAGService(nil, nil)
	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	facts := []dagcore.Fact{
		{ID: "f1", Predicate: "asserts", Args: []string{"issue-1", "ready", "true"}, GroundedBy: []string{"src-a"}},
		{ID: "f2", Predicate: "asserts", Args: []string{"issue-1", "ready", "false"}, GroundedBy: []string{"src-b"}},
	}
	conflicts, err := svc.DetectConflicts(context.Background(), wsID, facts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Severity != "requires_review" {
		t.Fatalf("unexpected severity: %s", conflicts[0].Severity)
	}
}
