package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cascade"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// startCascadeBackground constructs and starts the cascade worker +
// reconciliation cron in their own goroutines. Called once from
// NewRouterWithOptions after the TaskService is fully wired.
//
// No-ops when MULTICA_CASCADE_WEBHOOK_ENABLED is off OR when
// MULTICA_CASCADE_WORKSPACE_ID is missing — the worker can't lookup
// issues without a workspace context, so we fail-loud-at-startup
// rather than silently scope-skipping every event.
//
// The goroutines run under context.Background() for the process
// lifetime. Graceful shutdown of cascade work is a follow-up; the
// router doesn't currently expose a shutdown context to threads.
func startCascadeBackground(pool *pgxpool.Pool, queries *db.Queries, taskSvc *service.TaskService, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if !cascadeFlagEnabled() {
		return
	}

	workspaceID := strings.TrimSpace(os.Getenv("MULTICA_CASCADE_WORKSPACE_ID"))
	if workspaceID == "" {
		logger.Warn("cascade.wiring.no_workspace",
			"hint", "set MULTICA_CASCADE_WORKSPACE_ID to enable cascade worker")
		return
	}
	wsUUID, err := uuid.Parse(workspaceID)
	if err != nil {
		logger.Error("cascade.wiring.invalid_workspace_id",
			"value", workspaceID, "error", err)
		return
	}

	spawner := &taskServiceSpawner{queries: queries, taskSvc: taskSvc}
	loader := &queriesIssueLoader{queries: queries, workspaceID: wsUUID}
	worker := cascade.NewWorker(pool, spawner, loader, logger)

	// Reconciler nudge: log only at this wiring level. The
	// notify.Bridge (PR6) is the proper surface for off-platform
	// nudges; wiring the bridge requires Slack/Telegram env vars +
	// a CommentPoster adapter, which lands in a separate follow-up
	// alongside per-workspace channel routing. For now the cron
	// logs the stuck-cascade event so it shows up in the structured
	// log pipeline and observability picks it up.
	reconciler := cascade.NewReconciler(pool, func(_ context.Context, r cascade.StuckCascadeReport) {
		logger.Warn("cascade.stuck_detected",
			"issue_id", r.IssueID,
			"issue_number", r.IssueNumber,
			"last_event_at", r.LastEventAt,
			"staleness_hours", r.StalenessHours,
		)
	}, logger)

	go worker.Run(context.Background())
	go reconciler.Run(context.Background())

	logger.Info("cascade.wiring.started",
		"workspace_id", workspaceID,
		"github_real_adapter", os.Getenv("MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT") != "")
}

// cascadeFlagEnabled mirrors webhooks.envEnabled but lives in main
// so we don't import a second time. Truthy values match the
// webhooks package parser.
func cascadeFlagEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_CASCADE_WEBHOOK_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// taskServiceSpawner adapts service.TaskService to the
// cascade.Spawner interface. Spawn loads the issue (the queue
// enqueue needs a full db.Issue) and calls EnqueueTaskForIssue;
// HasActiveRun delegates to the existing queries.
type taskServiceSpawner struct {
	queries *db.Queries
	taskSvc *service.TaskService
}

func (s *taskServiceSpawner) Spawn(ctx context.Context, issueID uuid.UUID, _ cascade.TriggerContext) error {
	issue, err := s.queries.GetIssue(ctx, pgtype.UUID{Bytes: issueID, Valid: true})
	if err != nil {
		return fmt.Errorf("cascade spawner: load issue: %w", err)
	}
	if _, err := s.taskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
		return fmt.Errorf("cascade spawner: enqueue: %w", err)
	}
	return nil
}

func (s *taskServiceSpawner) HasActiveRun(ctx context.Context, issueID uuid.UUID) (bool, error) {
	active, err := s.queries.HasActiveTaskForIssue(ctx, pgtype.UUID{Bytes: issueID, Valid: true})
	if err != nil {
		return false, fmt.Errorf("cascade spawner: has-active query: %w", err)
	}
	return active, nil
}

// queriesIssueLoader resolves a "PUL-N" identifier to an issue UUID
// by parsing the trailing number and calling GetIssueByNumber. The
// workspace is fixed at construction (single-tenant assumption from
// MULTICA_CASCADE_WORKSPACE_ID); multi-workspace routing is a
// follow-up that needs a repo→workspace mapping table.
type queriesIssueLoader struct {
	queries     *db.Queries
	workspaceID uuid.UUID
}

func (l *queriesIssueLoader) LookupByIdentifier(ctx context.Context, identifier string) (uuid.UUID, error) {
	// Identifier shape is "PREFIX-N" by contract from
	// cascade.LookupIssueIdentifier. Split on the last dash.
	dash := strings.LastIndex(identifier, "-")
	if dash <= 0 || dash == len(identifier)-1 {
		return uuid.Nil, fmt.Errorf("cascade loader: malformed identifier %q", identifier)
	}
	numStr := identifier[dash+1:]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cascade loader: parse number from %q: %w", identifier, err)
	}
	issue, err := l.queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: pgtype.UUID{Bytes: l.workspaceID, Valid: true},
		Number:      int32(num),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, cascade.ErrIssueNotFound
		}
		return uuid.Nil, err
	}
	return uuid.UUID(issue.ID.Bytes), nil
}
