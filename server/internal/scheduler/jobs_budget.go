package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// JobNameBudgetAccounting is the canonical name of the budget job. Stable across
// releases: renaming it orphans its audit rows.
const JobNameBudgetAccounting = "budget_accounting"

// JobNameAutopilotQuietDispatch is the canonical name of the sweep that
// dispatches the routine runs quiet hours deferred.
const JobNameAutopilotQuietDispatch = "autopilot_quiet_dispatch"

// budgetUsagePassLimit bounds the tasks one minute's pass accounts. A larger
// backlog is taken over several minutes; the cursor keeps its place.
const budgetUsagePassLimit = 500

// BudgetAccountingJob runs once a minute. It books the cost of tasks whose usage
// changed since its cursor into the budget periods, releases the holds of tasks
// that ended and the holds of periods that rolled. The cursor lives in the
// process and starts a day back, so a restart re-reads recent usage; the
// accounting is idempotent, so a re-read adds nothing.
func BudgetAccountingJob(accounting *service.BudgetAccounting, queries *db.Queries, now func() time.Time) JobSpec {
	cursor := service.NewBudgetUsageCursor(now())
	return JobSpec{
		Name:              JobNameBudgetAccounting,
		Cadence:           time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        2 * time.Minute,
		StaleTimeout:      5 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			next, accounted, err := accounting.RunUsagePass(ctx, cursor, budgetUsagePassLimit)
			cursor = next
			if err != nil {
				return HandlerResult{}, fmt.Errorf("budget usage pass: %w", err)
			}
			ended, err := accounting.ReleaseEndedTaskHolds(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("release ended task holds: %w", err)
			}
			rolled, err := releaseRolledBudgetHolds(ctx, queries, accounting, now())
			if err != nil {
				return HandlerResult{}, err
			}
			return HandlerResult{RowsAffected: int64(accounted), Result: map[string]any{
				"tasks_accounted":     accounted,
				"holds_ended":         ended,
				"holds_period_rolled": rolled,
			}}, nil
		},
	}
}

func releaseRolledBudgetHolds(ctx context.Context, queries *db.Queries, accounting *service.BudgetAccounting, now time.Time) (int64, error) {
	workspaceIDs, err := queries.ListActiveBudgetWorkspaces(ctx)
	if err != nil {
		return 0, fmt.Errorf("list budget workspaces: %w", err)
	}
	var total int64
	for _, ws := range workspaceIDs {
		n, err := accounting.ReleasePeriodRolledHolds(ctx, ws, now)
		if err != nil {
			return total, fmt.Errorf("release rolled holds of %s: %w", util.UUIDToString(ws), err)
		}
		total += n
	}
	return total, nil
}

// DeferredAutopilotReleaser is the narrow contract the quiet dispatch sweep
// needs from service.AutopilotService.
type DeferredAutopilotReleaser interface {
	ReleaseQuietDeferredRuns(ctx context.Context, limit int32) (int, error)
}

// AutopilotQuietDispatchJob runs once a minute and dispatches the routine runs
// that quiet hours deferred, once their window has closed.
func AutopilotQuietDispatchJob(releaser DeferredAutopilotReleaser) JobSpec {
	return JobSpec{
		Name:              JobNameAutopilotQuietDispatch,
		Cadence:           time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        2 * time.Minute,
		StaleTimeout:      5 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := releaser.ReleaseQuietDeferredRuns(ctx, 200)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("release deferred runs: %w", err)
			}
			return HandlerResult{RowsAffected: int64(n), Result: map[string]any{"dispatched": n}}, nil
		},
	}
}
