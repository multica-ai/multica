// Package service: MemoryHub Guardian action adapter.
//
// This is the production Guardian boundary. It may request a binding action,
// invoke the ledger-aware rerun path, read back persisted evidence, and repair
// a blocked review. It may NOT write agent_task_queue directly or shell out to
// the CLI.
//
// V6-1/V6-2: RepairBlockedReviewer is the single owner-repair entry, exposed
// through the HTTP route POST /api/memoryhub/evidence/{execution_id}/review-repair.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Review repair errors (V6-1.2).
var (
	ErrExecutionEvidenceNotFound   = errors.New("memoryhub: execution_evidence_not_found")
	ErrReviewRepairForbidden       = errors.New("memoryhub: memoryhub_review_repair_forbidden")
	ErrReviewTransitionConflict    = errors.New("memoryhub: memoryhub_review_transition_conflict")
	ErrReviewerSelfForbidden       = errors.New("memoryhub: memoryhub_reviewer_self_forbidden")
	ErrReviewerScopeMismatch       = errors.New("memoryhub: memoryhub_reviewer_scope_mismatch")
)

// RepairBlockedReviewer is the V5-7.3 transaction exposed via the V6-1 route.
//
// Validation order (fail-closed):
//  1. caller has already passed RequireWorkspaceOwnerOrAdmin (403 handled in handler);
//  2. service loads the evidence record with execution_id + workspace_id (404);
//  3. the proposed reviewer exists, is active, in the same workspace, and is
//     not the execution agent (422);
//  4. one CAS transaction: WHERE review_state='blocked' AND review_version=$expected
//     (409 on zero rows) sets reviewer, clears review task/output/failure/lease/
//     owner-action, resets attempt, sets pending, wakeup=now, version++, inserts
//     the audit row; and
//  5. the scheduler wakeup is published only AFTER commit.
//
// CAS at-most-once: a replayed request observes pending/newer version and
// returns 409 with no second audit, no second notification, no mutation.
func (s *MemoryHubService) RepairBlockedReviewer(
	ctx context.Context,
	q *db.Queries,
	executionID pgtype.UUID,
	workspaceID string,
	expectedReviewVersion int32,
	reviewerAgentID pgtype.UUID,
	executionAgentID pgtype.UUID,
) (*db.ExecutionEvidenceRecord, error) {
	// 2. load record scoped to the actor workspace; cross-workspace is 404.
	if _, err := q.GetExecutionEvidenceRecordScoped(ctx, db.GetExecutionEvidenceRecordScopedParams{
		ExecutionID: executionID,
		WorkspaceID: uuidFromString(workspaceID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExecutionEvidenceNotFound
		}
		return nil, err
	}

	// 3. the proposed reviewer must exist, be active, belong to the same
	//    workspace, and differ from the execution agent (422).
	if reviewerAgentID.Valid {
		reviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          reviewerAgentID,
			WorkspaceID: uuidFromString(workspaceID),
		})
		if err != nil {
			return nil, ErrReviewerScopeMismatch
		}
		// Active reviewers are idle/working; archived/error/offline/blocked
		// reviewers cannot be assigned.
		if !isActiveReviewer(reviewer) {
			return nil, ErrReviewerScopeMismatch
		}
	}
	if executionAgentID.Valid && reviewerAgentID.Valid && executionAgentID == reviewerAgentID {
		return nil, ErrReviewerSelfForbidden
	}

	// 4. single CAS transaction.
	updated, err := q.RepairBlockedReviewerCAS(ctx, db.RepairBlockedReviewerCASParams{
		ExecutionID:     executionID,
		ReviewVersion:   expectedReviewVersion,
		ReviewerAgentID: reviewerAgentID,
		WorkspaceID:     uuidFromString(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrReviewTransitionConflict
		}
		return nil, err
	}
	return &updated, nil
}

var _ = fmt.Sprintf

// isActiveReviewer reports whether an agent can be assigned as an independent
// reviewer: it must be online/working (not archived, blocked, offline, or
// error). The agent status enum is idle|working|blocked|error|offline.
func isActiveReviewer(a db.Agent) bool {
	switch a.Status {
	case "idle", "working":
		return true
	default:
		return false
	}
}
