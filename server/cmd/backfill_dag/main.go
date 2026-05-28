// backfill_dag seeds the DAG projection tables from existing issue and
// issue_dependency rows. Run once after migration 108 ships.
//
// Idempotent: re-running safely re-upserts the same projections because
// record/link projections use deterministic keys.
//
// Usage:
//   go run ./cmd/backfill_dag
//
// Environment:
//   DATABASE_URL — postgres connection string (default local dev)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/dagcore"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dryRun = flag.Bool("dry-run", false, "log what would be backfilled without writing")
		batch  = flag.Int("batch", 1000, "issues per batch")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	queries := db.New(pool)
	dagSvc := service.NewDAGService(queries, pool)

	return backfill(ctx, pool, queries, dagSvc, *dryRun, *batch)
}

func backfill(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, dagSvc *service.DAGService, dryRun bool, batchSize int) error {
	totalIssues := 0
	totalDeps := 0
	offset := 0

	for {
		issues, err := q.ListIssues(ctx, db.ListIssuesParams{
			Limit:  int32(batchSize),
			Offset: int32(offset),
		})
		if err != nil {
			return fmt.Errorf("list issues: %w", err)
		}
		if len(issues) == 0 {
			break
		}

		for _, issue := range issues {
			recordID := issue.ID.String()
			wsID := issue.WorkspaceID

			if dryRun {
				slog.Info("would project issue", "record_id", recordID, "workspace", wsID)
			} else {
				// Create a synthetic creation event for the issue
				event := dagcore.Event{
					ID:        fmt.Sprintf("backfill-issue-%s", recordID),
					RecordIDs: []string{recordID},
					AgentID:   "backfill",
					DVT: dagcore.DVT{
						Dot:     dagcore.Dot{AgentID: "backfill", Counter: int64(totalIssues + 1)},
						Context: map[string]int64{"backfill": int64(totalIssues + 1)},
					},
					Operation: dagcore.OperationCreate,
					Payload:   map[string]any{"type": "issue", "status": issue.Status, "priority": issue.Priority},
					Reason:    "backfill from issue table",
				}
				_, err := dagSvc.AppendEvent(ctx, wsID, event)
				if err != nil {
					return fmt.Errorf("append issue event %s: %w", recordID, err)
				}
			}
			totalIssues++
		}

		offset += len(issues)
		slog.Info("backfill progress", "issues_processed", totalIssues)
	}

	// Backfill dependencies
	offset = 0
	for {
		// We need a raw query for issue_dependency; for now use a simpler approach
		// by iterating all link types from the dependency table.
		// Since sqlc doesn't have a ListIssueDependencies query, we'll do a raw query.
		rows, err := q.ListIssues(ctx, db.ListIssuesParams{Limit: 1, Offset: 0})
		if err != nil {
			return fmt.Errorf("health check: %w", err)
		}
		_ = rows

		// For the backfill we need to query issue_dependency directly.
		// The sqlc generated code doesn't expose a list-all query, so we use
		// the pool directly for this one-shot backfill.
		break
	}

	// Use raw query for dependencies since there's no list-all sqlc query.
	// issue_dependency does not have workspace_id; we join with issue to get it.
	const depQuery = `
		SELECT d.id, i.workspace_id, d.issue_id, d.depends_on_issue_id, d.type
		FROM issue_dependency d
		JOIN issue i ON i.id = d.issue_id
		ORDER BY d.id
		LIMIT $1 OFFSET $2
	`
	for {
		rows, err := pool.Query(ctx, depQuery, batchSize, offset)
		if err != nil {
			return fmt.Errorf("query dependencies: %w", err)
		}

		count := 0
		for rows.Next() {
			var depID pgtype.UUID
			var wsID, issueID, dependsOnID pgtype.UUID
			var depType string
			if err := rows.Scan(&depID, &wsID, &issueID, &dependsOnID, &depType); err != nil {
				rows.Close()
				return fmt.Errorf("scan dependency: %w", err)
			}

			fromID := issueID.String()
			toID := dependsOnID.String()

			if dryRun {
				slog.Info("would project dependency", "from", fromID, "to", toID, "type", depType)
			} else {
				event := dagcore.Event{
					ID:        fmt.Sprintf("backfill-dep-%s", depID.String()),
					RecordIDs: []string{fromID, toID},
					AgentID:   "backfill",
					DVT: dagcore.DVT{
						Dot:     dagcore.Dot{AgentID: "backfill", Counter: int64(totalDeps + 1)},
						Context: map[string]int64{"backfill": int64(totalDeps + 1)},
					},
					Operation: dagcore.OperationLink,
					Payload:   map[string]any{"link_type": depType},
					Reason:    "backfill from issue_dependency table",
				}
				_, err := dagSvc.AppendEvent(ctx, wsID, event)
				if err != nil {
					rows.Close()
					return fmt.Errorf("append dep event %s: %w", depID.String(), err)
				}
			}
			totalDeps++
			count++
		}
		rows.Close()

		if count == 0 {
			break
		}
		offset += count
		slog.Info("backfill progress", "deps_processed", totalDeps)
	}

	slog.Info("backfill complete", "issues", totalIssues, "dependencies", totalDeps)
	return nil
}
