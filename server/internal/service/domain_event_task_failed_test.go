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

// FailTasksInTxWithEvents is the shared mechanism behind every bulk task.failed
// path (the three runtime sweepers + daemon orphan recovery). Failing a task
// through it must persist a task.failed domain event atomically with the fail
// (MUL-4332 review point 2), stamped with the resolved workspace and issue.
func TestFailTasksInTxWithEventsEmitsTaskFailed(t *testing.T) {
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

	failed, err := svc.FailTasksInTxWithEvents(ctx, func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
		row, ferr := qtx.FailAgentTask(ctx, db.FailAgentTaskParams{
			ID:            util.MustParseUUID(taskID),
			Error:         pgtype.Text{String: "runtime went offline", Valid: true},
			FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
		})
		if ferr != nil {
			return nil, ferr
		}
		return []db.AgentTaskQueue{row}, nil
	})
	if err != nil {
		t.Fatalf("FailTasksInTxWithEvents: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed task, got %d", len(failed))
	}

	// Exactly one task.failed event for the task, carrying the issue + error.
	var evtType, payload string
	if err := pool.QueryRow(ctx,
		`SELECT type, payload::text FROM domain_event WHERE subject_type = 'task' AND subject_id = $1`,
		taskID).Scan(&evtType, &payload); err != nil {
		t.Fatalf("expected a task.failed domain event: %v", err)
	}
	if evtType != "task.failed" {
		t.Errorf("type = %q, want task.failed", evtType)
	}
	if !strings.Contains(payload, issueID) {
		t.Errorf("payload %s should carry issue_id %s", payload, issueID)
	}
	if !strings.Contains(payload, "runtime_offline") {
		t.Errorf("payload %s should carry the failure reason", payload)
	}
}
