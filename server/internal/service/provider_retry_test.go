package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestTransientProviderFailureRetries(t *testing.T) {
	cases := []struct {
		name, message string
		reason        taskfailure.Reason
	}{
		{"overloaded stream", "stream disconnected before completion: Our servers are currently overloaded. Please try again later.", taskfailure.ReasonAgentProviderCapacityOrRateLimit},
		{"rate limit", "HTTP 429 Too Many Requests", taskfailure.ReasonAgentProviderCapacityOrRateLimit},
		{"server error", "HTTP 503 Service Unavailable", taskfailure.ReasonAgentProviderServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := taskfailure.Classify(tc.message)
			if reason != tc.reason {
				t.Fatalf("Classify = %q, want %q", reason, tc.reason)
			}
			task := db.AgentTaskQueue{
				Attempt: 1, MaxAttempts: 2,
				IssueID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			}
			if !retryEligible(reason.String(), task) {
				t.Fatal("temporary provider failure ended the issue run instead of scheduling a retry")
			}
			if ResumeUnsafeFailure(reason.String(), tc.message) {
				t.Fatal("temporary provider failure must preserve the resumable session")
			}
			for _, budget := range []int32{1, 2, 5} {
				if got := retryAttemptCeiling(reason.String(), budget); got != budget {
					t.Errorf("attempt ceiling = %d, want configured budget %d", got, budget)
				}
				task.Attempt, task.MaxAttempts = budget, budget
				if retryEligible(reason.String(), task) {
					t.Errorf("retried with exhausted or disabled budget %d", budget)
				}
			}
			task.Attempt, task.MaxAttempts = 1, 2
			task.AutopilotRunID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
			if retryEligible(reason.String(), task) {
				t.Fatal("run-only Autopilot must retain its own retry cadence")
			}
			task.AutopilotRunID = pgtype.UUID{}
			task.ChatSessionID, task.IssueID = task.IssueID, pgtype.UUID{}
			if !retryEligible(reason.String(), task) {
				t.Fatal("chat provider failure must also retry")
			}
			task.ChatSessionID = pgtype.UUID{}
			if retryEligible(reason.String(), task) {
				t.Fatal("task without an issue or chat must not retry")
			}
		})
	}
}

func TestTransientProviderRetryBackoff(t *testing.T) {
	for _, reason := range []taskfailure.Reason{taskfailure.ReasonAgentProviderCapacityOrRateLimit, taskfailure.ReasonAgentProviderServerError} {
		for _, tc := range []struct {
			attempt  int32
			min, max time.Duration
		}{
			{1, 30 * time.Second, time.Minute},
			{2, time.Minute, 2 * time.Minute},
			{3, 2 * time.Minute, 4 * time.Minute},
			{4, 150 * time.Second, 5 * time.Minute},
			{1 << 30, 150 * time.Second, 5 * time.Minute},
		} {
			if got := retryDelayForAttempt(reason.String(), tc.attempt); got < tc.min || got > tc.max {
				t.Errorf("%s attempt %d: delay %s, want [%s, %s]", reason, tc.attempt, got, tc.min, tc.max)
			}
		}
	}
}

func TestPermanentProviderFailuresDoNotRetry(t *testing.T) {
	task := db.AgentTaskQueue{Attempt: 1, MaxAttempts: 5, IssueID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}}
	for _, message := range []string{"HTTP 401 Unauthorized", "HTTP 402 insufficient_balance", "HTTP 429 insufficient_quota"} {
		if reason := taskfailure.Classify(message); retryEligible(reason.String(), task) {
			t.Errorf("permanent provider failure %q classified as %s must not retry", message, reason)
		}
	}
}

func TestProviderRetryPersistence(t *testing.T) {
	for _, path := range []string{"failure report", "orphan recovery"} {
		t.Run(path, func(t *testing.T) {
			pool := newResolveOriginatorPool(t)
			ctx := context.Background()
			q := db.New(pool)
			workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
			fx := testutil.New(pool, workspaceID, userID)
			var runtimeID string
			fx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id=$1`, agentID).Scan(&runtimeID)
			fx.Exec(t, `UPDATE agent_runtime SET last_seen_at=now() WHERE id=$1`, runtimeID)
			parentID := util.MustParseUUID(fx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
				"attempt": 1, "max_attempts": 2,
				"session_id": "provider-session", "work_dir": "/tmp/provider-workdir",
			}))
			fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE parent_task_id=$1`, parentID)
			fx.Cleanup(t, `DELETE FROM comment WHERE issue_id=$1`, issueID)
			bus := events.New()
			var failure events.Event
			bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) { failure = e })
			svc := NewTaskService(q, pool, nil, bus)
			const message = "stream disconnected before completion: Our servers are currently overloaded. Please try again later."
			before := time.Now()
			if path == "failure report" {
				for range 2 {
					if _, err := svc.FailTask(ctx, parentID, message, "provider-session", "/tmp/provider-workdir", "", "", false, "", ""); err != nil {
						t.Fatalf("FailTask: %v", err)
					}
				}
				payload, ok := failure.Payload.(map[string]any)
				if !ok || payload["retry_pending"] != true {
					t.Fatalf("failure notification = %+v, want retry_pending=true", failure)
				}
				if n := fx.Count(t, `SELECT count(*) FROM comment WHERE issue_id=$1`, issueID); n != 0 {
					t.Fatalf("posted %d terminal comments while a retry is pending", n)
				}
			} else {
				fx.Exec(t, `UPDATE agent_task_queue SET status='failed', failure_reason=$2 WHERE id=$1`, parentID, taskfailure.ReasonAgentProviderCapacityOrRateLimit.String())
				parent, err := q.GetAgentTask(ctx, parentID)
				if err != nil {
					t.Fatal(err)
				}
				if child, err := svc.MaybeRetryFailedTask(ctx, parent); err != nil || child == nil {
					t.Fatalf("orphan recovery = %+v, %v; want a persisted retry", child, err)
				}
			}
			after := time.Now()
			if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, parentID); n != 1 {
				t.Fatalf("retry children = %d, want exactly one", n)
			}
			var childID pgtype.UUID
			fx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE parent_task_id=$1`, parentID).Scan(&childID)
			child, err := q.GetAgentTask(ctx, childID)
			if err != nil {
				t.Fatal(err)
			}
			if child.Status != "deferred" || !child.FireAt.Valid || child.FireAt.Time.Before(before.Add(30*time.Second)) || child.FireAt.Time.After(after.Add(time.Minute)) {
				t.Fatalf("retry status=%s fire_at=%v, want a persisted 30-60 second delay", child.Status, child.FireAt)
			}
			if child.Attempt != 2 || child.MaxAttempts != 2 || child.SessionID.String != "provider-session" || child.WorkDir.String != "/tmp/provider-workdir" || child.ForceFreshSession {
				t.Fatalf("retry lost its attempt budget or resume context: attempt=%d max=%d session=%s workdir=%s fresh=%v", child.Attempt, child.MaxAttempts, child.SessionID.String, child.WorkDir.String, child.ForceFreshSession)
			}
			// Recreate the service to prove the cooldown lives in Postgres.
			svc = NewTaskService(q, pool, nil, bus)
			if claimed, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(runtimeID)); err != nil || claimed != nil {
				t.Fatalf("early claim = %+v, %v; cooldown must prevent execution", claimed, err)
			}
			fx.Exec(t, `UPDATE agent_task_queue SET fire_at=now()-interval '1 second' WHERE id=$1`, childID)
			claimed, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(runtimeID))
			if err != nil || claimed == nil || claimed.ID != childID {
				t.Fatalf("due claim = %+v, %v; want the original retry", claimed, err)
			}
			if _, err := svc.FailTask(ctx, childID, message, "provider-session", "/tmp/provider-workdir", "", "", false, "", ""); err != nil {
				t.Fatalf("exhaust final attempt: %v", err)
			}
			if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, childID); n != 0 {
				t.Fatalf("exhausted budget created %d extra attempts", n)
			}
			payload, ok := failure.Payload.(map[string]any)
			if !ok || payload["retry_pending"] != false || payload["failure_reason"] != taskfailure.ReasonAgentProviderCapacityOrRateLimit.String() {
				t.Fatalf("exhausted retry must publish terminal provider failure, got %+v", failure)
			}
		})
	}
}
