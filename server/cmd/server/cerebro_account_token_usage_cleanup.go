package main

// CEREBRO-PATCH(cerebro-account-token-retention): cleanup rolling-window token telemetry.

import (
	"context"
	"log/slog"
	"time"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

const cerebroAccountTokenUsageCleanupInterval = 6 * time.Hour

func runCerebroAccountTokenUsageCleanup(ctx context.Context, queries *cerebrodb.Queries) {
	cleanupOldCerebroAccountTokenUsage(ctx, queries)

	ticker := time.NewTicker(cerebroAccountTokenUsageCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupOldCerebroAccountTokenUsage(ctx, queries)
		}
	}
}

func cleanupOldCerebroAccountTokenUsage(ctx context.Context, queries *cerebrodb.Queries) {
	deleted, err := queries.DeleteOldCerebroAccountTokenUsage(ctx)
	if err != nil {
		slog.Warn("cerebro account token usage cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("cerebro account token usage cleanup deleted old rows", "rows", deleted)
	}
}
