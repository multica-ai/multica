package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClaimCommentIdempotencySideEffectsUsesSingleLiveLease(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createCommentTriggerPreviewIssue(t, "comment idempotency claim lease", "", "")
	commentID := dbfx.Comment(t, issueID, "A comment with a replay claim.", nil)
	key := "claim-" + uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment_idempotency (
			workspace_id, idempotency_key, request_hash, comment_id,
			attachment_ids, suppress_agent_ids
		)
		VALUES ($1, $2, $3, $4, '{}'::uuid[], '{}'::uuid[])
	`, testWorkspaceID, key, strings.Repeat("c", 64), commentID); err != nil {
		t.Fatalf("insert claim fixture: %v", err)
	}

	args := db.ClaimCommentIdempotencySideEffectsParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		IdempotencyKey: key,
		RequestHash:    strings.Repeat("c", 64),
		LeaseBefore:    pgtype.Timestamptz{Time: time.Now().UTC().Add(-commentIdempotencySideEffectsLease), Valid: true},
	}
	claimed, err := testHandler.Queries.ClaimCommentIdempotencySideEffects(context.Background(), args)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("first claim changed %d rows, want 1", claimed)
	}
	claimed, err = testHandler.Queries.ClaimCommentIdempotencySideEffects(context.Background(), args)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claimed != 0 {
		t.Fatalf("second live claim changed %d rows, want 0", claimed)
	}

	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment_idempotency
		SET side_effects_claimed_at = now() - interval '11 minutes'
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, key); err != nil {
		t.Fatalf("age claim fixture: %v", err)
	}
	claimed, err = testHandler.Queries.ClaimCommentIdempotencySideEffects(context.Background(), args)
	if err != nil {
		t.Fatalf("expired claim takeover: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("expired claim changed %d rows, want 1", claimed)
	}
}

func TestReconcilePendingCommentIdempotencyCompletesInterruptedSideEffects(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createCommentTriggerPreviewIssue(t, "comment idempotency recovery", "", "")
	commentID := dbfx.Comment(t, issueID, "A comment whose downstream effects need recovery.", nil)
	key := "recovery-" + uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment_idempotency (
			workspace_id, idempotency_key, request_hash, comment_id,
			attachment_ids, suppress_agent_ids, created_at
		)
		VALUES ($1, $2, $3, $4, '{}'::uuid[], '{}'::uuid[], $5)
	`, testWorkspaceID, key, strings.Repeat("b", 64), commentID, time.Now().UTC()); err != nil {
		t.Fatalf("insert interrupted idempotency row: %v", err)
	}

	recovered, err := testHandler.ReconcilePendingCommentIdempotency(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReconcilePendingCommentIdempotency() error = %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	var completedAt *time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT side_effects_completed_at
		FROM comment_idempotency
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, key).Scan(&completedAt); err != nil {
		t.Fatalf("read recovered idempotency row: %v", err)
	}
	if completedAt == nil {
		t.Fatal("side_effects_completed_at is still NULL after recovery")
	}
}

func TestReconcilePendingCommentIdempotencyDeadLettersPermanentAttachmentMismatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := createCommentTriggerPreviewIssue(t, "comment idempotency permanent attachment", "", "")
	targetID := createHandlerTestAgent(t, "Recovery Target", nil)
	commentID := dbfx.Comment(t, issueID, fmt.Sprintf(
		"[@Target](mention://agent/%s) please inspect this recovery comment.", targetID,
	), nil)
	key := "recovery-permanent-attachment-" + uuid.NewString()
	badAttachmentID := uuid.New()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment_idempotency (
			workspace_id, idempotency_key, request_hash, comment_id,
			attachment_ids, suppress_agent_ids, created_at
		)
		VALUES ($1, $2, $3, $4, ARRAY[$5::uuid], '{}'::uuid[], $6)
	`, testWorkspaceID, key, strings.Repeat("a", 64), commentID, badAttachmentID.String(), time.Now().UTC()); err != nil {
		t.Fatalf("insert permanent attachment fixture: %v", err)
	}

	recovered, err := testHandler.ReconcilePendingCommentIdempotency(context.Background(), 10)
	if err != nil {
		t.Fatalf("first ReconcilePendingCommentIdempotency() error = %v", err)
	}
	if recovered != 1 {
		t.Fatalf("first recovered = %d, want 1", recovered)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, targetID); got != 1 {
		t.Fatalf("first recovery queued tasks = %d, want 1", got)
	}

	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment_idempotency
		SET side_effects_claimed_at = now() - interval '11 minutes'
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, key); err != nil {
		t.Fatalf("age dead-letter fixture: %v", err)
	}
	recovered, err = testHandler.ReconcilePendingCommentIdempotency(context.Background(), 10)
	if err != nil {
		t.Fatalf("second ReconcilePendingCommentIdempotency() error = %v", err)
	}
	if recovered != 0 {
		t.Fatalf("second recovered = %d, want 0 after dead-letter completion", recovered)
	}

	var completedAt *time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT side_effects_completed_at
		FROM comment_idempotency
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, key).Scan(&completedAt); err != nil {
		t.Fatalf("read dead-letter idempotency row: %v", err)
	}
	if completedAt == nil {
		t.Fatal("side_effects_completed_at is still NULL after permanent attachment mismatch")
	}
}
