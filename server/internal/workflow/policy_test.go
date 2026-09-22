package workflow

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRetryDelayUsesBoundedExponentialBackoff(t *testing.T) {
	policy := DefaultRetryPolicy()
	if got := RetryDelay(policy, 1, 0); got != 30*time.Second {
		t.Fatalf("first delay = %s", got)
	}
	if got := RetryDelay(policy, 2, 0); got != 60*time.Second {
		t.Fatalf("second delay = %s", got)
	}
	if got := RetryDelay(policy, 20, 0); got != 15*time.Minute {
		t.Fatalf("bounded delay = %s", got)
	}
}

func TestShouldRetryRequiresSafeReplay(t *testing.T) {
	policy := DefaultRetryPolicy()
	if ShouldRetry("network_temporary", policy, 0) {
		t.Fatal("unknown effect must not be automatically replayed")
	}
	policy.EffectState = "read_only"
	if !ShouldRetry("network_temporary", policy, 0) {
		t.Fatal("safe read-only operation should be replayed")
	}
	if ShouldRetry("network_temporary", policy, 2) {
		t.Fatal("retry budget must be enforced")
	}
	if ShouldRetry("output_invalid", policy, 0) {
		t.Fatal("output validation errors are business failures, not retryable network errors")
	}
}

func TestTaskTimeoutReasonDistinguishesQueueAndExecution(t *testing.T) {
	old := time.Now().UTC().Add(-2 * time.Minute)
	defaults := WorkflowDefaults{QueueTimeoutSeconds: 60, ExecutionTimeoutSeconds: 60}
	queued, message := taskTimeoutReason(dbTaskWithTimes(old, time.Time{}), "queued", defaults)
	if queued != "queue_timeout" || message == "" {
		t.Fatalf("queued timeout = %q, %q", queued, message)
	}
	running, message := taskTimeoutReason(dbTaskWithTimes(time.Time{}, old), "running", defaults)
	if running != "execution_timeout" || message == "" {
		t.Fatalf("execution timeout = %q, %q", running, message)
	}
}

func dbTaskWithTimes(created, started time.Time) db.AgentTaskQueue {
	return db.AgentTaskQueue{
		CreatedAt: pgtype.Timestamptz{Time: created, Valid: !created.IsZero()},
		StartedAt: pgtype.Timestamptz{Time: started, Valid: !started.IsZero()},
	}
}
