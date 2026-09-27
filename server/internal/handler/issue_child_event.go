package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

// processChildEvents runs the sub-issue rules of these parents (the child_done
// system rule and people's sub-issue conditions) right after a write. The
// write recorded its sub-issue changes in its own transaction; a failure here
// is retried by the scheduler sweep.
func (h *Handler) processChildEvents(ctx context.Context, parentIDs ...pgtype.UUID) {
	_ = (&service.IssueWakeupService{Tasks: h.TaskService}).ProcessChildEvents(ctx, parentIDs...)
}
