package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

const capacityRateLimit = string(taskfailure.ReasonAgentProviderCapacityOrRateLimit)

// TestCapacityRateLimitConstantsInSync pins capacityRateLimitMaxAttempts to
// 1 (original run) + len(capacityRateLimitBackoff). If the backoff schedule
// grows or shrinks, the ceiling must move with it, otherwise the last (or a
// non-existent) tier is never reached.
func TestCapacityRateLimitConstantsInSync(t *testing.T) {
	if want := int32(1 + len(capacityRateLimitBackoff)); capacityRateLimitMaxAttempts != want {
		t.Fatalf("capacityRateLimitMaxAttempts = %d, want 1+len(backoff) = %d",
			capacityRateLimitMaxAttempts, want)
	}
}

// TestProviderCapacityRateLimitIsRetryable guards the MS-121 contract: a
// classified rate/session-limit failure must be in the auto-retry allowlist.
func TestProviderCapacityRateLimitIsRetryable(t *testing.T) {
	if !retryableReasons[capacityRateLimit] {
		t.Fatal("provider_capacity_or_rate_limit must be retryable (MS-121)")
	}
	// Quota (402 / insufficient balance / credits) is terminal and must stay
	// non-retryable — waiting does not top up a balance.
	if retryableReasons[string(taskfailure.ReasonAgentProviderQuotaLimit)] {
		t.Fatal("provider_quota_limit must NOT be retryable")
	}
}

func TestRetryAttemptCeilingCapacityRateLimit(t *testing.T) {
	cases := []struct {
		name        string
		reason      string
		maxAttempts int32
		want        int32
	}{
		{"widens default 2 to 4", capacityRateLimit, 2, capacityRateLimitMaxAttempts},
		{"keeps higher explicit budget", capacityRateLimit, 6, 6},
		{"never revives a disabled task", capacityRateLimit, 1, 1},
		{"unrelated reason keeps column", "timeout", 2, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := retryAttemptCeiling(c.reason, c.maxAttempts); got != c.want {
				t.Errorf("retryAttemptCeiling(%q, %d) = %d, want %d", c.reason, c.maxAttempts, got, c.want)
			}
		})
	}
}

func TestRetryDelayForAttemptCapacityRateLimit(t *testing.T) {
	cases := []struct {
		failedAttempt int32
		want          time.Duration
	}{
		{1, 15 * time.Minute},
		{2, time.Hour},
		{3, 4 * time.Hour},
		{4, 0}, // past the schedule: ceiling reached, no further child
	}
	for _, c := range cases {
		if got := retryDelayForAttempt(capacityRateLimit, c.failedAttempt); got != c.want {
			t.Errorf("retryDelayForAttempt(capacity, attempt=%d) = %s, want %s", c.failedAttempt, got, c.want)
		}
	}
}

func issueTask(reason string, attempt, maxAttempts int32) db.AgentTaskQueue {
	return db.AgentTaskQueue{
		IssueID:       pgtype.UUID{Valid: true},
		Attempt:       attempt,
		MaxAttempts:   maxAttempts,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	}
}

func TestRetryEligibleCapacityRateLimit(t *testing.T) {
	// Fresh issue run that just hit a rate limit on its first attempt.
	if !retryEligible(capacityRateLimit, issueTask(capacityRateLimit, 1, 2)) {
		t.Error("first-attempt rate-limited issue run should be retry-eligible")
	}
	// Budget exhausted at the reason-aware ceiling (4).
	if retryEligible(capacityRateLimit, issueTask(capacityRateLimit, capacityRateLimitMaxAttempts, 2)) {
		t.Error("run at the ceiling must not be retry-eligible")
	}
	// max_attempts <= 1 disables retry entirely.
	if retryEligible(capacityRateLimit, issueTask(capacityRateLimit, 1, 1)) {
		t.Error("max_attempts=1 disables auto-retry")
	}
	// Chat turns are excluded from this reason's hours-long backoff.
	chat := issueTask(capacityRateLimit, 1, 2)
	chat.IssueID = pgtype.UUID{}
	chat.ChatSessionID = pgtype.UUID{Valid: true}
	if retryEligible(capacityRateLimit, chat) {
		t.Error("chat task must not get the capacity/rate-limit staggered backoff")
	}
	// Autopilot owns its own cadence.
	ap := issueTask(capacityRateLimit, 1, 2)
	ap.AutopilotRunID = pgtype.UUID{Valid: true}
	if retryEligible(capacityRateLimit, ap) {
		t.Error("autopilot task must not be auto-retried here")
	}
}
