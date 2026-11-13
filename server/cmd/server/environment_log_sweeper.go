package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const environmentLogSweepInterval = time.Minute

// runEnvironmentLogSweeper deletes the environment log reads the sidebar no
// longer needs, so program output does not stay in the database.
func runEnvironmentLogSweeper(ctx context.Context, queries *db.Queries) {
	ticker := time.NewTicker(environmentLogSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if n, err := handler.SweepEnvironmentLogCommands(sweepCtx, queries, time.Now()); err != nil {
				slog.Warn("environment log sweep failed", "error", err)
			} else if n > 0 {
				slog.Debug("environment log sweep removed reads", "count", n)
			}
			cancel()
		}
	}
}
