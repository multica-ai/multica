package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type IssueQueuePromoter interface {
	PromoteQueuedIssues(ctx context.Context, workspaceID pgtype.UUID, anyScope bool) (int, error)
}

// IssueQueueSweepJob promotes queued backlog issues whose turn has come. The
// status-change listener covers the common case at once; this catches the
// writes that publish no such event (agent task completion, GitHub sync, a
// deleted predecessor) and any promotion the listener missed.
func IssueQueueSweepJob(promoter IssueQueuePromoter) JobSpec {
	return JobSpec{
		Name: "issue_queue_sweep", Cadence: 30 * time.Second, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			_, err := promoter.PromoteQueuedIssues(ctx, pgtype.UUID{}, true)
			return HandlerResult{}, err
		},
	}
}
