package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var (
	errInvalidCommentCreateRequestID   = errors.New("invalid comment create request id")
	errCommentCreateRequestConflict    = errors.New("comment create request key was reused with different content")
	errCommentCreateRequestGone        = errors.New("comment created by this request was deleted")
	errCommentTriggerDeliveryRetryable = errors.New("comment trigger delivery has retryable targets")
)

func prepareCommentCreateRequestIdentity(
	req CreateCommentRequest,
	issueID string,
	parentID pgtype.UUID,
	attachmentIDs []pgtype.UUID,
	suppressAgentIDs []pgtype.UUID,
	sourceTaskID pgtype.UUID,
) (pgtype.Text, pgtype.Text, error) {
	if req.RequestID == nil {
		return pgtype.Text{}, pgtype.Text{}, nil
	}
	key := strings.TrimSpace(*req.RequestID)
	if !issueCreateRequestIDPattern.MatchString(key) {
		return pgtype.Text{}, pgtype.Text{}, errInvalidCommentCreateRequestID
	}
	payload := struct {
		IssueID          string   `json:"issue_id"`
		Content          string   `json:"content"`
		Type             string   `json:"type"`
		ParentID         *string  `json:"parent_id"`
		AttachmentIDs    []string `json:"attachment_ids"`
		SuppressAgentIDs []string `json:"suppress_agent_ids"`
		SourceTaskID     *string  `json:"source_task_id"`
	}{
		IssueID:          issueID,
		Content:          req.Content,
		Type:             req.Type,
		ParentID:         canonicalOptionalUUID(parentID),
		AttachmentIDs:    canonicalUUIDStrings(attachmentIDs),
		SuppressAgentIDs: canonicalUUIDStrings(suppressAgentIDs),
		SourceTaskID:     canonicalOptionalUUID(sourceTaskID),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return pgtype.Text{}, pgtype.Text{}, err
	}
	digest := sha256.Sum256(encoded)
	return pgtype.Text{String: key, Valid: true}, pgtype.Text{String: fmt.Sprintf("%x", digest), Valid: true}, nil
}

func isCommentCreateRequestCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "idx_comment_create_request"
}

const (
	commentTriggerOutboxLeaseSeconds = 60
	commentTriggerOutboxMaxAttempts  = 10
)

// CommentTriggerOutboxDeliveryResult describes durable comment-to-agent
// routing. A saved comment and its trigger intent commit together; this worker
// may run inline for latency or later after a crash without losing the input.
type CommentTriggerOutboxDeliveryResult struct {
	Claimed int
	Done    int
	Failed  int
}

func (h *Handler) retryCommentTriggerOutbox(ctx context.Context, row db.CommentTriggerOutbox, owner string, deliveryErr error) {
	message := fmt.Sprintf("%T", deliveryErr)
	_, err := h.Queries.RetryCommentTriggerOutbox(ctx, db.RetryCommentTriggerOutboxParams{
		MaxAttempts:            commentTriggerOutboxMaxAttempts,
		NextAttemptAt:          pgtype.Timestamptz{Time: time.Now().UTC().Add(15 * time.Second), Valid: true},
		LastError:              pgtype.Text{String: message, Valid: true},
		CommentID:              row.CommentID,
		CommentTriggerRevision: row.CommentTriggerRevision,
		LeaseOwner:             pgtype.Text{String: owner, Valid: true},
	})
	if err != nil {
		slog.Warn("comment trigger outbox retry update failed", "comment_id", uuidToString(row.CommentID), "error", err)
	}
}

func (h *Handler) deliverClaimedCommentTriggerOutbox(ctx context.Context, row db.CommentTriggerOutbox, owner string) ([]CommentTriggerOutcome, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	// Revalidate ownership and the exact input generation under a row lock.
	// The lock stays held through routing, coverage checks, and task writes, so
	// a lease takeover cannot interleave after a stale consumer's pre-check.
	locked, err := qtx.LockCommentTriggerOutboxForDelivery(ctx, db.LockCommentTriggerOutboxForDeliveryParams{
		CommentID:              row.CommentID,
		CommentTriggerRevision: row.CommentTriggerRevision,
		LeaseOwner:             pgtype.Text{String: owner, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("comment trigger outbox lease or revision was lost")
	}
	if err != nil {
		return nil, err
	}
	row = locked

	comment, err := qtx.GetCommentInWorkspaceForTriggerDelivery(ctx, db.GetCommentInWorkspaceForTriggerDeliveryParams{
		ID: row.CommentID, WorkspaceID: row.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, markErr := qtx.MarkCommentTriggerOutboxDone(ctx, db.MarkCommentTriggerOutboxDoneParams{
			CommentID: row.CommentID, CommentTriggerRevision: row.CommentTriggerRevision,
			LeaseOwner: pgtype.Text{String: owner, Valid: true},
		})
		if markErr != nil {
			return nil, markErr
		}
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	if comment.TriggerRevision != row.CommentTriggerRevision {
		return nil, errors.New("comment trigger outbox input revision changed")
	}
	if comment.DeletedAt.Valid {
		_, err = qtx.MarkCommentTriggerOutboxDone(ctx, db.MarkCommentTriggerOutboxDoneParams{
			CommentID: row.CommentID, CommentTriggerRevision: row.CommentTriggerRevision,
			LeaseOwner: pgtype.Text{String: owner, Valid: true},
		})
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}
	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: row.IssueID, WorkspaceID: row.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	var parent *db.Comment
	if comment.ParentID.Valid {
		loaded, getErr := qtx.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID: comment.ParentID, WorkspaceID: row.WorkspaceID,
		})
		if getErr == nil {
			parent = &loaded
		} else if !errors.Is(getErr, pgx.ErrNoRows) {
			return nil, getErr
		}
	}
	originator := ""
	if row.OriginatorUserID.Valid {
		originator = uuidToString(row.OriginatorUserID)
	}
	txTaskService := h.TaskService.WithTransactionQueries(qtx)
	txHandler := *h
	txHandler.Queries = qtx
	txHandler.TaskService = txTaskService
	outcomes, blockedTargets, deliveryErr := txHandler.resolveCommentTriggerDelivery(
		ctx, issue, comment, parent, row.ActorType, uuidToString(row.ActorID),
		originator, row.SuppressAgentIds,
	)
	if deliveryErr != nil {
		return outcomes, deliveryErr
	}
	updated, err := qtx.MarkCommentTriggerOutboxDone(ctx, db.MarkCommentTriggerOutboxDoneParams{
		CommentID: row.CommentID, CommentTriggerRevision: row.CommentTriggerRevision,
		LeaseOwner: pgtype.Text{String: owner, Valid: true},
	})
	if err != nil {
		return outcomes, err
	}
	if updated != 1 {
		return outcomes, errors.New("comment trigger outbox lease was lost")
	}
	if err := tx.Commit(ctx); err != nil {
		return outcomes, err
	}
	for _, task := range txTaskService.TransactionEnqueuedTasks() {
		h.TaskService.BroadcastTaskQueued(ctx, task)
		h.TaskService.NotifyTaskEnqueued(ctx, task)
	}
	h.noteBlockedRuntimeTargets(ctx, issue, blockedTargets)
	return outcomes, nil
}

func (h *Handler) processCommentTriggerOutbox(ctx context.Context, commentID pgtype.UUID) ([]CommentTriggerOutcome, error) {
	if _, err := h.Queries.ExpireExhaustedCommentTriggerOutbox(ctx, commentTriggerOutboxMaxAttempts); err != nil {
		return nil, err
	}
	owner := uuidToString(dbid.NewV7())
	row, err := h.Queries.ClaimCommentTriggerOutboxByComment(ctx, db.ClaimCommentTriggerOutboxByCommentParams{
		LeaseOwner:   pgtype.Text{String: owner, Valid: true},
		LeaseSeconds: commentTriggerOutboxLeaseSeconds,
		MaxAttempts:  commentTriggerOutboxMaxAttempts,
		CommentID:    commentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	outcomes, err := h.deliverClaimedCommentTriggerOutbox(ctx, row, owner)
	if err != nil {
		h.retryCommentTriggerOutbox(ctx, row, owner, err)
	}
	return outcomes, err
}

// DeliverCommentTriggerOutbox repairs comments committed before their routing
// step completed. Trigger creation itself is already keyed by comment IDs, so
// an expired lease can be retried without manufacturing a second instruction.
func (h *Handler) DeliverCommentTriggerOutbox(ctx context.Context, limit int) (CommentTriggerOutboxDeliveryResult, error) {
	var result CommentTriggerOutboxDeliveryResult
	if limit <= 0 {
		return result, nil
	}
	if _, err := h.Queries.ExpireExhaustedCommentTriggerOutbox(ctx, commentTriggerOutboxMaxAttempts); err != nil {
		return result, err
	}
	owner := uuidToString(dbid.NewV7())
	rows, err := h.Queries.ClaimCommentTriggerOutbox(ctx, db.ClaimCommentTriggerOutboxParams{
		LeaseOwner:   pgtype.Text{String: owner, Valid: true},
		LeaseSeconds: commentTriggerOutboxLeaseSeconds,
		MaxAttempts:  commentTriggerOutboxMaxAttempts,
		RowLimit:     int32(limit),
	})
	if err != nil {
		return result, err
	}
	result.Claimed = len(rows)
	for _, row := range rows {
		if _, err := h.deliverClaimedCommentTriggerOutbox(ctx, row, owner); err != nil {
			result.Failed++
			h.retryCommentTriggerOutbox(ctx, row, owner, err)
			continue
		}
		result.Done++
	}
	return result, nil
}
