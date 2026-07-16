package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FailBulkTasksWithEvents is the shared mechanism behind every bulk task.failed
// path (the runtime sweepers + daemon orphan recovery). Failing a task through it
// must persist a task.failed domain event atomically with the fail (MUL-4332
// review point 2), stamped with the resolved workspace and issue, attributed to
// the platform (SystemActor, not the agent) and carrying a retryable flag that
// agrees with the auto-retry decision (review point 3).
func TestFailBulkTasksWithEventsEmitsTaskFailed(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	// Seed a running task for that agent/issue.
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'running', 0)
		RETURNING id`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
			row, gerr := qtx.GetAgentTask(ctx, util.MustParseUUID(taskID))
			if gerr != nil {
				return nil, gerr
			}
			return []db.AgentTaskQueue{row}, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "runtime went offline", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("FailBulkTasksWithEvents: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed task, got %d", len(failed))
	}

	// Exactly one task.failed event for the task, carrying the issue + error,
	// attributed to the platform, with a retry_eligible flag matching the shared
	// retryEligible predicate.
	var evtType, actorType, payload string
	var retryEligibleFlag bool
	if err := pool.QueryRow(ctx,
		`SELECT type, actor_type, (payload->>'retry_eligible')::bool, payload::text
		 FROM domain_event WHERE subject_type = 'task' AND subject_id = $1`,
		taskID).Scan(&evtType, &actorType, &retryEligibleFlag, &payload); err != nil {
		t.Fatalf("expected a task.failed domain event: %v", err)
	}
	if evtType != "task.failed" {
		t.Errorf("type = %q, want task.failed", evtType)
	}
	if actorType != "system" {
		t.Errorf("actor_type = %q, want system (a sweeper fail is platform-driven, not the agent's action)", actorType)
	}
	if want := retryEligible("runtime_offline", failed[0]); retryEligibleFlag != want {
		t.Errorf("retry_eligible = %v, want %v (event must agree with the eligibility predicate)", retryEligibleFlag, want)
	}
	if !strings.Contains(payload, issueID) {
		t.Errorf("payload %s should carry issue_id %s", payload, issueID)
	}
	if !strings.Contains(payload, "runtime_offline") {
		t.Errorf("payload %s should carry the failure reason", payload)
	}
}

// task.failed.retry_eligible is an atomically-decidable eligibility fact committed
// WITH the fail — never a promise that a retry child exists (Elon review round 3,
// point 2). On the bulk sweeper / orphan-recovery paths the event commits inside
// FailBulkTasksWithEvents while the retry child is only created best-effort AFTER
// commit by HandleFailedTasks, where CreateRetryTask can error or the process can
// crash. We reproduce that crash window by committing the fail+event and then NOT
// running the post-commit retry step: the flag must still equal the eligibility
// predicate, the task must be terminal, and no retry child may exist — so a consumer
// can never read retry_eligible as "a fresh attempt is guaranteed to arrive".
func TestTaskFailedRetryEligibleIsNotAChildPromise(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	cases := []struct {
		name         string
		attempt      int
		maxAttempts  int
		wantEligible bool
	}{
		// A within-budget, infra-shaped failure IS eligible — yet the crash window
		// means no child exists after commit until HandleFailedTasks runs.
		{"within budget → eligible, still no child in the window", 1, 2, true},
		// A budget-exhausted failure is NOT eligible: the flag is decidable both
		// ways, it is not hard-coded true.
		{"budget exhausted → not eligible", 2, 2, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var taskID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts)
				VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'running', 0, $3, $4)
				RETURNING id`, agentID, issueID, tc.attempt, tc.maxAttempts).Scan(&taskID); err != nil {
				t.Fatalf("seed task: %v", err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1 OR parent_task_id = $1`, taskID)
				pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
			})

			failed, err := svc.FailBulkTasksWithEvents(ctx,
				func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
					row, gerr := qtx.GetAgentTask(ctx, util.MustParseUUID(taskID))
					if gerr != nil {
						return nil, gerr
					}
					return []db.AgentTaskQueue{row}, nil
				},
				func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
					return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
						Ids:           ids,
						Error:         pgtype.Text{String: "runtime went offline", Valid: true},
						FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
					})
				})
			if err != nil {
				t.Fatalf("FailBulkTasksWithEvents: %v", err)
			}
			if len(failed) != 1 {
				t.Fatalf("expected 1 failed task, got %d", len(failed))
			}

			// Deliberately DO NOT call HandleFailedTasks: that post-commit step is
			// where the retry child would be created. Skipping it models a crash in
			// exactly that window.

			// The committed event's flag equals the eligibility predicate...
			var got bool
			if err := pool.QueryRow(ctx,
				`SELECT (payload->>'retry_eligible')::bool FROM domain_event
				 WHERE subject_type = 'task' AND subject_id = $1`, taskID).Scan(&got); err != nil {
				t.Fatalf("expected a task.failed domain event: %v", err)
			}
			if got != tc.wantEligible {
				t.Errorf("retry_eligible = %v, want %v", got, tc.wantEligible)
			}
			// ...the subject task is terminal...
			if s := taskStatusForTest(t, pool, taskID); s != "failed" {
				t.Errorf("task status = %q, want failed (task.failed is terminal for the subject)", s)
			}
			// ...and no retry child exists — the flag is not a child promise.
			var children int
			if err := pool.QueryRow(ctx,
				`SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, taskID).Scan(&children); err != nil {
				t.Fatalf("count retry children: %v", err)
			}
			if children != 0 {
				t.Errorf("retry children = %d, want 0 (no child is created in the crash window)", children)
			}
		})
	}
}
