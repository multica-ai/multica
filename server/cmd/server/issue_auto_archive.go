package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// autoArchiveInterval is how often the auto-archive sweeper runs.
	autoArchiveInterval = 1 * time.Hour

	// autoArchiveBatchSize caps the number of issues archived per sweep tick.
	autoArchiveBatchSize = 100
)

// runIssueAutoArchive periodically scans for terminal-state issues (done,
// cancelled) that have been untouched for more than 30 days and archives them.
func runIssueAutoArchive(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	ticker := time.NewTicker(autoArchiveInterval)
	defer ticker.Stop()

	// Run once at startup, then on each tick.
	sweepAutoArchiveIssues(ctx, queries, bus)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepAutoArchiveIssues(ctx, queries, bus)
		}
	}
}

// sweepAutoArchiveIssues finds terminal-state issues older than 30 days that
// are not yet archived and archives them in a single batch. Each archived
// issue emits an issue:archived WebSocket event so other clients see the
// change in real time.
func sweepAutoArchiveIssues(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("auto-archive sweeper panic", "error", r)
		}
	}()

	issues, err := queries.SelectAutoArchiveCandidates(ctx, autoArchiveBatchSize)
	if err != nil {
		slog.Warn("auto-archive sweeper: failed to select candidates", "error", err)
		return
	}
	if len(issues) == 0 {
		return
	}

	archived := 0
	for _, issue := range issues {
		archivedIssue, err := queries.ArchiveIssue(ctx, db.ArchiveIssueParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			ArchivedBy:  pgtype.UUID{}, // system-initiated, NULL
		})
		if err != nil {
			slog.Warn("auto-archive sweeper: failed to archive issue",
				"error", err,
				"issue_id", util.UUIDToString(issue.ID),
			)
			continue
		}
		archived++

		bus.Publish(events.Event{
			Type:        protocol.EventIssueArchived,
			WorkspaceID: util.UUIDToString(archivedIssue.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"issue": map[string]any{
					"id":     util.UUIDToString(archivedIssue.ID),
					"number": archivedIssue.Number,
				},
			},
		})
	}

	if archived > 0 {
		slog.Info("auto-archive sweeper: archived issues", "count", archived)
	}
}
