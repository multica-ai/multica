package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestFailTask_MalformedClaimToken verifies that a malformed (non-UUID)
// claim_token is rejected with ErrInvalidClaimToken rather than being
// silently dropped (which would bypass token validation).
func TestFailTask_MalformedClaimToken(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	mock := &mockDBTX{task: db.AgentTaskQueue{
		ID:      taskID,
		AgentID: agentID,
		Status:  "running",
	}}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	_, err := svc.FailTask(context.Background(), taskID, "crash", "", "", "", "not-a-uuid")
	if err == nil {
		t.Fatal("expected error for malformed claim token, got nil")
	}
	if !errors.Is(err, ErrInvalidClaimToken) {
		t.Fatalf("expected ErrInvalidClaimToken, got %v", err)
	}
}

// TestFailTask_TokenlessOnTokenedRow verifies that a tokenless FailTask
// request cannot fail a task that has a claim_token set. The SQL returns
// no rows (token mismatch), and the service should return ErrClaimTokenInvalid
// because the task is still active.
func TestFailTask_TokenlessOnTokenedRow(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)
	claimToken := testUUID(3)

	// The mock returns ErrNoRows for the UPDATE (simulating the SQL not
	// matching because claim_token IS NOT NULL but no token was supplied),
	// and returns the task with status=dispatched for the lookup.
	mock := &mockDBTX{task: db.AgentTaskQueue{
		ID:         taskID,
		AgentID:    agentID,
		Status:     "dispatched",
		ClaimToken: claimToken,
	}}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	// Empty claimToken = tokenless request
	_, err := svc.FailTask(context.Background(), taskID, "crash", "", "", "", "")
	if err == nil {
		t.Fatal("expected error for tokenless FailTask on tokened row, got nil")
	}
	if !errors.Is(err, ErrClaimTokenInvalid) {
		t.Fatalf("expected ErrClaimTokenInvalid, got %v", err)
	}
}

// TestFailTask_WrongTokenOnTokenedRow verifies that a valid but wrong
// claim_token cannot fail a task owned by another lease.
func TestFailTask_WrongTokenOnTokenedRow(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)
	realToken := testUUID(3)

	mock := &mockDBTX{task: db.AgentTaskQueue{
		ID:         taskID,
		AgentID:    agentID,
		Status:     "running",
		ClaimToken: realToken,
	}}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	// Use a different valid UUID as the wrong token
	wrongToken := pgtype.UUID{Valid: true}
	wrongToken.Bytes[0] = 99

	_, err := svc.FailTask(context.Background(), taskID, "crash", "", "", "", uuidToStr(wrongToken))
	if err == nil {
		t.Fatal("expected error for wrong claim token, got nil")
	}
	if !errors.Is(err, ErrClaimTokenInvalid) {
		t.Fatalf("expected ErrClaimTokenInvalid, got %v", err)
	}
}

func uuidToStr(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
