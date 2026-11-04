package scheduler

import (
	"context"
	"time"
)

// JobNameWeeklyPolicyReview is the canonical name used in audit rows.
const JobNameWeeklyPolicyReview = "policy_weekly_review"

// WeeklyPolicyReviewRunner records the policy section of each workspace's weekly
// review (spec 4.4).
type WeeklyPolicyReviewRunner interface {
	RunWeeklyPolicyReview(ctx context.Context, now time.Time) (int, error)
}

// WeeklyPolicyReviewJob runs once a week. A run that is retried finds the week
// already recorded and writes nothing more.
func WeeklyPolicyReviewJob(runner WeeklyPolicyReviewRunner) JobSpec {
	return JobSpec{
		Name:              JobNameWeeklyPolicyReview,
		Cadence:           7 * 24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     7 * 24 * time.Hour,
		RunTimeout:        20 * time.Minute,
		StaleTimeout:      30 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{5 * time.Minute, 15 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := runner.RunWeeklyPolicyReview(ctx, time.Now())
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
