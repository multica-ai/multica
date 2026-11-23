package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Comment creation honours an optional Idempotency-Key header (GH #8221).
//
// A client whose POST timed out cannot tell whether the comment was saved: the
// request may have died on the way in, or the server may have committed the
// comment and lost the response on the way out. Without a key, resending in
// the second case posts the comment twice and re-fires its mentions. With a
// key, the server records a receipt in the comment's own transaction, and a
// retry with the same key from the same author gets the saved comment back
// (200, Idempotent-Replayed: true) with no new row and no new side effects.
const (
	commentIdempotencyKeyHeader      = "Idempotency-Key"
	commentIdempotencyReplayedHeader = "Idempotent-Replayed"
	commentIdempotencyMaxKeyLength   = 255
	// Receipts only need to outlive a client's retry window. A week covers a
	// long network outage plus a human rerunning the command the next day.
	commentIdempotencyRetention  = 7 * 24 * time.Hour
	commentIdempotencySweepBatch = 1000
)

var errCommentIdempotencyClaimed = errors.New("comment idempotency key already claimed")

type commentIdempotency struct {
	key         db.GetCommentIdempotencyReceiptParams
	issueID     pgtype.UUID
	fingerprint []byte
}

// commentIdempotencyFromRequest returns nil when the request carries no key.
// Keys are scoped to the author, so two authors can never collide and a key
// can never reveal another author's comment.
//
// The fingerprint covers where the comment goes (issue, parent, type) but not
// its content: the CLI uploads attachments again on every rerun and rewrites
// the body's file links to the new uploads, so the same logical comment can
// arrive with a different body. A key reused for a different target is still
// refused.
func commentIdempotencyFromRequest(w http.ResponseWriter, r *http.Request, issue db.Issue, parentID pgtype.UUID, commentType, authorType, authorID string) (*commentIdempotency, bool) {
	key := strings.TrimSpace(r.Header.Get(commentIdempotencyKeyHeader))
	if key == "" {
		return nil, true
	}
	if len(key) > commentIdempotencyMaxKeyLength {
		writeError(w, http.StatusBadRequest, "Idempotency-Key is too long")
		return nil, false
	}
	payload, _ := json.Marshal(struct {
		IssueID  string `json:"issue_id"`
		ParentID string `json:"parent_id"`
		Type     string `json:"type"`
	}{uuidToString(issue.ID), uuidToString(parentID), commentType})
	sum := sha256.Sum256(payload)
	return &commentIdempotency{
		key: db.GetCommentIdempotencyReceiptParams{
			WorkspaceID:    issue.WorkspaceID,
			ActorType:      authorType,
			ActorID:        parseUUID(authorID),
			IdempotencyKey: key,
		},
		issueID:     issue.ID,
		fingerprint: sum[:],
	}, true
}

// claim records the receipt inside the comment's transaction. It must run
// before the comment insert so a concurrent request with the same key waits
// on the unique index instead of inserting a second comment.
func (c *commentIdempotency) claim(ctx context.Context, q *db.Queries, commentID pgtype.UUID) error {
	if c == nil {
		return nil
	}
	n, err := q.ClaimCommentIdempotencyReceipt(ctx, db.ClaimCommentIdempotencyReceiptParams{
		WorkspaceID:        c.key.WorkspaceID,
		ActorType:          c.key.ActorType,
		ActorID:            c.key.ActorID,
		IdempotencyKey:     c.key.IdempotencyKey,
		IssueID:            c.issueID,
		CommentID:          commentID,
		RequestFingerprint: c.fingerprint,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errCommentIdempotencyClaimed
	}
	return nil
}

// replayIdempotentComment answers a request whose key already has a receipt.
// It reports false when no receipt exists and the caller should create the
// comment. Every other outcome has been written to w.
func (h *Handler) replayIdempotentComment(w http.ResponseWriter, r *http.Request, c *commentIdempotency) bool {
	if c == nil {
		return false
	}
	receipt, err := h.Queries.GetCommentIdempotencyReceipt(r.Context(), c.key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		slog.Warn("read comment idempotency receipt failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return true
	}
	if !bytes.Equal(receipt.RequestFingerprint, c.fingerprint) || uuidToString(receipt.IssueID) != uuidToString(c.issueID) {
		writeError(w, http.StatusUnprocessableEntity, "Idempotency-Key was already used for a comment on a different issue, thread or type")
		return true
	}
	comment, err := h.Queries.GetComment(r.Context(), receipt.CommentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("load idempotent comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(receipt.CommentID))...)
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return true
	}
	if errors.Is(err, pgx.ErrNoRows) || comment.DeletedAt.Valid || uuidToString(comment.WorkspaceID) != uuidToString(c.key.WorkspaceID) {
		// The original request succeeded and the comment was deleted since.
		// Posting it again would undo that deletion.
		writeError(w, http.StatusConflict, "the comment created with this Idempotency-Key has been deleted")
		return true
	}
	grouped := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	resp := commentToResponse(comment, nil, grouped[uuidToString(comment.ID)])
	slog.Info("comment create replayed", append(logger.RequestAttrs(r), "comment_id", uuidToString(comment.ID), "issue_id", uuidToString(comment.IssueID))...)
	w.Header().Set(commentIdempotencyReplayedHeader, "true")
	writeJSON(w, http.StatusOK, resp)
	return true
}

// SweepCommentIdempotencyReceipts deletes receipts older than the retention
// window in bounded batches. It returns the number of rows removed.
func SweepCommentIdempotencyReceipts(ctx context.Context, queries *db.Queries, now time.Time) (int64, error) {
	before := pgtype.Timestamptz{Time: now.Add(-commentIdempotencyRetention), Valid: true}
	var total int64
	for {
		n, err := queries.DeleteCommentIdempotencyReceiptsBefore(ctx, db.DeleteCommentIdempotencyReceiptsBeforeParams{
			Before:    before,
			BatchSize: commentIdempotencySweepBatch,
		})
		total += n
		if err != nil || n < commentIdempotencySweepBatch {
			return total, err
		}
	}
}
