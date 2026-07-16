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
	// attributed to the platform, with a retryable flag matching the shared
	// retryEligible predicate.
	var evtType, actorType, payload string
	var retryable bool
	if err := pool.QueryRow(ctx,
		`SELECT type, actor_type, (payload->>'retryable')::bool, payload::text
		 FROM domain_event WHERE subject_type = 'task' AND subject_id = $1`,
		taskID).Scan(&evtType, &actorType, &retryable, &payload); err != nil {
		t.Fatalf("expected a task.failed domain event: %v", err)
	}
	if evtType != "task.failed" {
		t.Errorf("type = %q, want task.failed", evtType)
	}
	if actorType != "system" {
		t.Errorf("actor_type = %q, want system (a sweeper fail is platform-driven, not the agent's action)", actorType)
	}
	if want := retryEligible("runtime_offline", failed[0]); retryable != want {
		t.Errorf("retryable = %v, want %v (event must agree with the auto-retry decision)", retryable, want)
	}
	if !strings.Contains(payload, issueID) {
		t.Errorf("payload %s should carry issue_id %s", payload, issueID)
	}
	if !strings.Contains(payload, "runtime_offline") {
		t.Errorf("payload %s should carry the failure reason", payload)
	}
}
