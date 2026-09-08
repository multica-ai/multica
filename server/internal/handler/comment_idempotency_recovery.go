package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CommentIdempotencyReplayWindow is the bounded period during which a client
// retry is guaranteed to replay the original comment rather than create a new
// one. It is also the maximum age of an interrupted side-effect row eligible
// for background recovery.
const CommentIdempotencyReplayWindow = 7 * 24 * time.Hour

const commentIdempotencyRecoveryBatchSize = 500

// commentIdempotencySideEffectsLease bounds ownership of a replay pass. A
// crashed process leaves the claim behind, so the sweeper can take it over
// after this interval without allowing healthy replicas to duplicate effects.
const commentIdempotencySideEffectsLease = 10 * time.Minute

// ReconcilePendingCommentIdempotency resumes keyed comment creates whose
// comment transaction committed but whose downstream effects did not reach a
// durable completion marker. The comment is never recreated; only the
// post-commit effects are retried.
func (h *Handler) ReconcilePendingCommentIdempotency(ctx context.Context, limit int) (int, error) {
	if h == nil || h.Queries == nil || h.TxStarter == nil || limit <= 0 {
		return 0, nil
	}
	if limit > commentIdempotencyRecoveryBatchSize {
		limit = commentIdempotencyRecoveryBatchSize
	}
	rows, err := h.Queries.ListPendingCommentIdempotency(ctx, db.ListPendingCommentIdempotencyParams{
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(-CommentIdempotencyReplayWindow), Valid: true},
		Limit:     int32(limit),
	})
	if err != nil {
		return 0, err
	}

	recovered := 0
	var firstErr error
	for _, row := range rows {
		ok, err := h.reconcilePendingCommentIdempotencyRow(ctx, row)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("comment idempotency recovery failed", "workspace_id", uuidToString(row.WorkspaceID), "idempotency_key", row.IdempotencyKey, "error", err)
			continue
		}
		if ok {
			recovered++
		}
	}
	return recovered, firstErr
}

func (h *Handler) reconcilePendingCommentIdempotencyRow(ctx context.Context, row db.ListPendingCommentIdempotencyRow) (bool, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin recovery transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCommentIdempotencyKey(ctx, uuidToString(row.WorkspaceID)+":"+row.IdempotencyKey); err != nil {
		return false, fmt.Errorf("lock idempotency key: %w", err)
	}
	stored, err := qtx.GetCommentIdempotency(ctx, db.GetCommentIdempotencyParams{
		WorkspaceID:    row.WorkspaceID,
		IdempotencyKey: row.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load idempotency row: %w", err)
	}
	if stored.SideEffectsCompletedAt.Valid {
		return false, nil
	}

	comment, err := qtx.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          stored.CommentID,
		WorkspaceID: row.WorkspaceID,
	})
	if err != nil {
		return false, fmt.Errorf("load idempotent comment: %w", err)
	}
	issue, err := qtx.GetIssue(ctx, comment.IssueID)
	if err != nil {
		return false, fmt.Errorf("load idempotent comment issue: %w", err)
	}
	if uuidToString(issue.WorkspaceID) != uuidToString(row.WorkspaceID) {
		return false, fmt.Errorf("comment idempotency row workspace does not match comment issue")
	}

	var parentComment, rootComment *db.Comment
	if comment.ParentID.Valid {
		parent, parentErr := qtx.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID:          comment.ParentID,
			WorkspaceID: row.WorkspaceID,
		})
		if parentErr != nil {
			return false, fmt.Errorf("load idempotent comment parent: %w", parentErr)
		}
		parentComment = &parent
		if root, rootErr := qtx.GetThreadRoot(ctx, db.GetThreadRootParams{
			CommentID:   comment.ParentID,
			WorkspaceID: row.WorkspaceID,
		}); rootErr == nil {
			rootComment = &root
		}
	}
	claimed, err := qtx.ClaimCommentIdempotencySideEffects(ctx, db.ClaimCommentIdempotencySideEffectsParams{
		WorkspaceID:    row.WorkspaceID,
		IdempotencyKey: row.IdempotencyKey,
		RequestHash:    row.RequestHash,
		LeaseBefore:    pgtype.Timestamptz{Time: time.Now().UTC().Add(-commentIdempotencySideEffectsLease), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("claim idempotency side effects: %w", err)
	}
	if claimed == 0 {
		return false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit recovery read: %w", err)
	}

	_, _, complete, terminal := h.runCommentPostCommitSideEffects(
		ctx,
		nil,
		issue,
		comment,
		issue.Revision,
		parentComment,
		rootComment,
		stored.AttachmentIds,
		stored.SuppressAgentIds,
	)
	if !complete && !terminal {
		return false, nil
	}
	return h.markCommentIdempotencySideEffectsCompleted(ctx, row.WorkspaceID, row.IdempotencyKey, row.RequestHash), nil
}
