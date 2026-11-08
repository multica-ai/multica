package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The status activity precedes dispatch and records a prediction. Runs and
// task:queued remain the evidence of actual enqueue. Only unsuccessful direct
// handoffs need an additional best-effort notice, bypassing comment triggers.
func (h *Handler) noteWorkflowHandoffEnqueueFailure(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, err error) {
	if err == nil && task.ID.Valid && task.ID.Bytes != [16]byte{} {
		return
	}
	reason := "Enqueue failed. Check the run list before trying again."
	if errors.Is(err, service.ErrDuplicatePendingTask) {
		reason = "Another pending run occupied the queue slot at enqueue time; this attempt did not create a new run."
	}
	// Use the write's snapshot, not a later issue read: another status move
	// may have happened while dispatch was in flight. Never include target
	// metadata or raw service errors in an issue-visible notice.
	content := fmt.Sprintf("For the workflow handoff to **%s** recorded at %s (revision %d): %s",
		escapeMarkdownInline(issue.Status), issue.UpdatedAt.Time.UTC().Format(time.RFC3339Nano), issue.Revision, reason)
	h.noteTriggerFailure(ctx, issue, content, pgtype.UUID{})
}
