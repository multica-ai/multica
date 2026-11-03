package scheduler

import (
	"context"
	"time"
)

// JobNameLibrarianBackfill is the canonical name used in audit rows.
const JobNameLibrarianBackfill = "librarian_backfill"

// LibrarianBackfiller adds the Librarian to workspaces set up before the starter
// company existed. It returns how many workspaces it changed.
type LibrarianBackfiller interface {
	BackfillLibrarian(ctx context.Context) (int, error)
}

// LibrarianBackfillJob runs hourly. Each step is idempotent, so a workspace that
// has its Librarian changes nothing, and a failed run is retried by the next one.
func LibrarianBackfillJob(backfiller LibrarianBackfiller) JobSpec {
	return JobSpec{
		Name:              JobNameLibrarianBackfill,
		Cadence:           time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := backfiller.BackfillLibrarian(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
