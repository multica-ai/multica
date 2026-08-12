package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestRepairBlockedReviewerSuccess verifies the V5-7.3 owner-repair CAS:
// a blocked record with matching review_version transitions to pending,
// review_version increments, and reviewer/task/output/failure/lease fields
// are cleared (reviewer reset to the new id, task/output null, attempt 0,
// wakeup set).
func TestRepairBlockedReviewerSuccess(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateBlocked), 3, 3, nil, 1)
	// stale lease/task/output fields that repair must clear
	if _, err := pool.Exec(ctx, `
		UPDATE execution_evidence_record
		SET review_lease_owner = 'old-owner',
		    review_lease_expires_at = now(),
		    review_task_id = gen_random_uuid(),
		    review_output_ref = '{"ref":"old"}'::jsonb,
		    review_failure_code = 'memoryhub_review_blocked',
		    review_next_wakeup = now()
		WHERE execution_id = $1
	`, execID); err != nil {
		t.Fatalf("set stale review fields: %v", err)
	}

	svc := NewMemoryHubService(q, nil)
	updated, err := svc.RepairBlockedReviewer(ctx, q, uuidFromString(execID), fx.workspaceID, 1, uuidFromString(fx.agentID), pgtype.UUID{})
	if err != nil {
		t.Fatalf("RepairBlockedReviewer: %v", err)
	}
	if updated.ReviewState != string(protocol.ReviewStatePending) {
		t.Fatalf("review_state = %q, want pending", updated.ReviewState)
	}
	if updated.ReviewVersion != 2 {
		t.Fatalf("review_version = %d, want 2", updated.ReviewVersion)
	}
	if updated.ReviewAttempt != 0 {
		t.Fatalf("review_attempt = %d, want 0", updated.ReviewAttempt)
	}
	if updated.ReviewLeaseOwner.Valid || updated.ReviewTaskID.Valid || updated.ReviewOutputRef != nil || updated.ReviewFailureCode.Valid {
		t.Fatalf("repair did not clear stale review fields: lease=%v task=%v output=%v failure=%v",
			updated.ReviewLeaseOwner.Valid, updated.ReviewTaskID.Valid, updated.ReviewOutputRef, updated.ReviewFailureCode.Valid)
	}
	if !updated.ReviewNextWakeup.Valid {
		t.Fatal("review_next_wakeup must be set after repair")
	}
}

// TestRepairBlockedReviewerConflict verifies a stale expected_review_version
// maps to 409 (at-most-once CAS: replay mutates nothing).
func TestRepairBlockedReviewerConflict(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateBlocked), 0, 3, nil, 1)
	svc := NewMemoryHubService(q, nil)

	if _, err := svc.RepairBlockedReviewer(ctx, q, uuidFromString(execID), fx.workspaceID, 99, uuidFromString(fx.agentID), pgtype.UUID{}); err != ErrReviewTransitionConflict {
		t.Fatalf("stale version err = %v, want ErrReviewTransitionConflict", err)
	}
}

// TestRepairBlockedReviewerNotFound verifies a cross-workspace / unknown
// execution maps to 404 with no lookup exposure.
func TestRepairBlockedReviewerNotFound(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	svc := NewMemoryHubService(q, nil)
	if _, err := svc.RepairBlockedReviewer(ctx, q, uuidFromString("00000000-0000-0000-0000-000000000000"), fx.workspaceID, 1, uuidFromString(fx.agentID), pgtype.UUID{}); err != ErrExecutionEvidenceNotFound {
		t.Fatalf("err = %v, want ErrExecutionEvidenceNotFound", err)
	}
}

// TestRepairBlockedReviewerScopeMismatch verifies an offline / cross-workspace
// reviewer maps to 422 (V6-1.3 reviewer validation).
func TestRepairBlockedReviewerScopeMismatch(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateBlocked), 0, 3, nil, 1)
	svc := NewMemoryHubService(q, nil)

	// offline reviewer is not active -> 422
	if _, err := svc.RepairBlockedReviewer(ctx, q, uuidFromString(execID), fx.workspaceID, 1, uuidFromString("00000000-0000-0000-0000-000000000000"), pgtype.UUID{}); err != ErrReviewerScopeMismatch {
		t.Fatalf("missing reviewer err = %v, want ErrReviewerScopeMismatch", err)
	}
}

// TestRepairBlockedReviewerSelfForbidden verifies the reviewer == execution
// agent maps to 422 (V6-1.3 self-review forbidden).
func TestRepairBlockedReviewerSelfForbidden(t *testing.T) {
	pool := newReviewSchedulerTestPool(t)
	ctx := context.Background()
	fx := newReviewSchedulerFixture(t, ctx, pool)
	q := db.New(pool)

	execID := insertEvidenceRecord(t, ctx, pool, fx.workspaceID, string(protocol.ReviewStateBlocked), 0, 3, nil, 1)
	svc := NewMemoryHubService(q, nil)

	// Same agent as reviewer and execution agent -> 422 self-forbidden
	// (before scope check, matching fail-closed ordering).
	_, err := svc.RepairBlockedReviewer(ctx, q, uuidFromString(execID), fx.workspaceID, 1, uuidFromString(fx.agentID), uuidFromString(fx.agentID))
	if err != ErrReviewerSelfForbidden {
		t.Fatalf("self-review err = %v, want ErrReviewerSelfForbidden", err)
	}
}
