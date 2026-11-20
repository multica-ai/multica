package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestShutdownRetryUsesExistingBudgetAndSessionPolicy(t *testing.T) {
	for _, tc := range []struct {
		reason       string
		attempt, max int32
		want         bool
	}{
		{"runtime_recovery", 1, 2, true},
		{"runtime_recovery", 2, 2, false},
		{"runtime_recovery", 1, 1, false},
		{"cancelled", 1, 2, false},
	} {
		task := db.AgentTaskQueue{Attempt: tc.attempt, MaxAttempts: tc.max, IssueID: pgtype.UUID{Valid: true}}
		if got := retryEligible(tc.reason, task); got != tc.want {
			t.Errorf("retryEligible(%s, %d/%d) = %v, want %v", tc.reason, tc.attempt, tc.max, got, tc.want)
		}
		if ceiling := retryAttemptCeiling(tc.reason, tc.max); ceiling != tc.max {
			t.Errorf("%s widened budget to %d", tc.reason, ceiling)
		}
	}
	if resumeUnsafeFailureReason("runtime_recovery") {
		t.Fatal("shutdown must preserve the resumable session")
	}
}
