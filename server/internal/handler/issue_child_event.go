package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

// processChildEvents runs the sub-issue rules of these parents (the child_done
// system rule and people's sub-issue conditions) right after a write. The
// write recorded its sub-issue changes in its own transaction; a failure here
// is retried by the scheduler sweep.
func (h *Handler) processChildEvents(ctx context.Context, parentIDs ...pgtype.UUID) {
	// A closed sub-issue can open the next stage of a parent the server
	// dispatches. That runs first: the wake for the stage that closed is
	// skipped only for a parent whose dispatch pass started a run. A failed
	// pass is retried by the stage dispatch sweep.
	var started []pgtype.UUID
	dispatch := h.StageDispatchService()
	if dispatch.Enabled(ctx) {
		for _, id := range parentIDs {
			if !id.Valid {
				continue
			}
			n, err := dispatch.Dispatch(ctx, id)
			if err != nil {
				slog.Warn("stage dispatch after a sub-issue change failed; the sweep retries", "parent_id", uuidToString(id), "error", err)
			}
			if n > 0 {
				started = append(started, id)
			}
		}
	}
	_ = (&service.IssueWakeupService{Tasks: h.TaskService}).ProcessChildEvents(service.WithStageDispatchStarted(ctx, started...), parentIDs...)
}
