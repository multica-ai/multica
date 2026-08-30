package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service/toolapprovalsweeper"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const toolApprovalSweepInterval = time.Minute

func runToolApprovalSweeper(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries) {
	sweeper := toolapprovalsweeper.New(toolapprovalsweeper.NewSQLStore(pool, queries), toolapprovalsweeper.Config{})
	run := func() {
		result, err := sweeper.RunOnce(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("tool approval sweeper failed", "error", err)
			}
			return
		}
		if result.Expired != 0 || result.ApprovalsDeleted != 0 || result.ActionEventsDeleted != 0 {
			slog.Info("tool approval sweeper completed",
				"expired", result.Expired,
				"approvals_deleted", result.ApprovalsDeleted,
				"action_events_deleted", result.ActionEventsDeleted,
			)
		}
	}
	run()
	ticker := time.NewTicker(toolApprovalSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
