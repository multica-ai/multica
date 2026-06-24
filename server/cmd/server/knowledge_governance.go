package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	knowledgeGovernanceInterval  = 6 * time.Hour
	knowledgeGovernanceBatchSize = 500
)

func runKnowledgeGovernance(ctx context.Context, queries *db.Queries) {
	ticker := time.NewTicker(knowledgeGovernanceInterval)
	defer ticker.Stop()

	sweepKnowledgeGovernance(ctx, queries)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepKnowledgeGovernance(ctx, queries)
		}
	}
}

func sweepKnowledgeGovernance(ctx context.Context, queries *db.Queries) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("knowledge governance sweeper panic", "error", r)
		}
	}()

	workspaceIDs, err := queries.ListAllWorkspaceIDs(ctx)
	if err != nil {
		slog.Warn("knowledge governance sweeper: failed to list workspaces", "error", err)
		return
	}
	if len(workspaceIDs) == 0 {
		return
	}

	svc := service.NewKnowledgeService(queries, nil)
	totalChecked := 0
	totalReviewNeeded := 0
	totalConflicts := 0
	for _, workspaceID := range workspaceIDs {
		result, err := svc.RunGovernance(ctx, service.KnowledgeGovernanceParams{
			WorkspaceID: workspaceID,
			Limit:       knowledgeGovernanceBatchSize,
		})
		if err != nil {
			slog.Warn("knowledge governance sweeper: workspace scan failed",
				"workspace_id", util.UUIDToString(workspaceID),
				"error", err,
			)
			continue
		}
		totalChecked += result.Checked
		totalReviewNeeded += result.ReviewNeeded
		totalConflicts += result.Conflicts
	}
	if totalChecked > 0 {
		slog.Info("knowledge governance sweeper: scanned knowledge",
			"workspaces", len(workspaceIDs),
			"checked", totalChecked,
			"review_needed", totalReviewNeeded,
			"conflicts", totalConflicts,
		)
	}
}
