package scheduler

import (
	"context"
	"time"
)

// JobNameScorecards, JobNameProbation and JobNameReviewRoutine are the canonical
// names used in audit rows. Stable across releases.
const (
	JobNameScorecards    = "scorecards"
	JobNameProbation     = "org_change_probation"
	JobNameReviewRoutine = "monthly_review"
)

// ScorecardRunner computes the nightly scorecards. It returns how many it wrote.
type ScorecardRunner interface {
	RunNightlyScorecards(ctx context.Context) (int, error)
}

// ScorecardJob runs once a day and writes the last completed week and month for
// every agent, squad and the company (spec 4.6). Each upsert replaces the period's
// row, so a retry is safe.
func ScorecardJob(runner ScorecardRunner) JobSpec {
	return JobSpec{
		Name:              JobNameScorecards,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        20 * time.Minute,
		StaleTimeout:      25 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := runner.RunNightlyScorecards(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}

// ProbationChecker judges the probation windows that have ended.
type ProbationChecker interface {
	CheckDueProbations(ctx context.Context) (int, error)
}

// ProbationJob runs once a day. A window that has ended gets its verdict:
// improved, neutral or worse, or stays open when its figures are not in yet
// (spec 4.6). A recorded outcome is never written again, so a rerun is safe.
func ProbationJob(checker ProbationChecker) JobSpec {
	return JobSpec{
		Name:              JobNameProbation,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := checker.CheckDueProbations(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}

// ReviewRoutineFiler files the monthly review issues.
type ReviewRoutineFiler interface {
	FileMonthlyReviews(ctx context.Context) (int, error)
}

// ReviewRoutineJob runs once a day and files the monthly review issue for each
// workspace's reviewer, once per month (spec 4.6).
func ReviewRoutineJob(filer ReviewRoutineFiler) JobSpec {
	return JobSpec{
		Name:              JobNameReviewRoutine,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := filer.FileMonthlyReviews(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
