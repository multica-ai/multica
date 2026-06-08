package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestShouldRetryAutopilotTask(t *testing.T) {
	runID := pgtype.UUID{Valid: true}
	runID.Bytes[0] = 1

	tests := []struct {
		name string
		task db.AgentTaskQueue
		want bool
	}{
		{
			name: "retries no-progress semantic inactivity once",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "codex_semantic_inactivity", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: true,
		},
		{
			name: "retries hard timeout once",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "timeout", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: true,
		},
		{
			name: "does not retry after budget exhausted",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "codex_semantic_inactivity", Valid: true},
				Attempt:        2,
				MaxAttempts:    2,
			},
			want: false,
		},
		{
			name: "does not retry agent errors",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "agent_error", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: false,
		},
		{
			name: "does not retry cancelled tasks",
			task: db.AgentTaskQueue{
				Status:         "cancelled",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "timeout", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: false,
		},
		{
			name: "does not retry non-autopilot tasks",
			task: db.AgentTaskQueue{
				Status:        "failed",
				FailureReason: pgtype.Text{String: "timeout", Valid: true},
				Attempt:       1,
				MaxAttempts:   2,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetryAutopilotTask(tc.task); got != tc.want {
				t.Fatalf("shouldRetryAutopilotTask() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAutopilotRetryThroughFailTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := openAutopilotRetryTestPool(t, ctx)
	queries := db.New(pool)
	bus := events.New()
	taskSvc := NewTaskService(queries, pool, nil, bus)
	autopilotSvc := NewAutopilotService(queries, pool, bus, taskSvc)
	if taskSvc.AutopilotSvc != autopilotSvc {
		t.Fatal("NewAutopilotService did not wire TaskService.AutopilotSvc")
	}

	_, _, agentID, runID, taskID := createAutopilotRetryFixture(t, ctx, pool)

	parent, err := taskSvc.FailTask(ctx, taskID, "first no-progress failure", "", "", "codex_semantic_inactivity")
	if err != nil {
		t.Fatalf("FailTask parent: %v", err)
	}
	if parent.Status != "failed" {
		t.Fatalf("parent status = %s, want failed", parent.Status)
	}

	run, err := queries.GetAutopilotRun(ctx, runID)
	if err != nil {
		t.Fatalf("load run after parent failure: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("run status after first failure = %s, want running", run.Status)
	}
	if !run.TaskID.Valid || run.TaskID == taskID {
		t.Fatalf("run task_id = %s, want retry task different from parent", uuidString(run.TaskID))
	}

	child, err := queries.GetAgentTask(ctx, run.TaskID)
	if err != nil {
		t.Fatalf("load retry task: %v", err)
	}
	if child.Status != "queued" {
		t.Fatalf("retry task status = %s, want queued", child.Status)
	}
	if child.Attempt != 2 || child.MaxAttempts != 2 {
		t.Fatalf("retry attempt/max = %d/%d, want 2/2", child.Attempt, child.MaxAttempts)
	}
	if !child.ParentTaskID.Valid || child.ParentTaskID != taskID {
		t.Fatalf("retry parent_task_id = %s, want %s", uuidString(child.ParentTaskID), uuidString(taskID))
	}

	// Replaying the old parent event must not enqueue a second retry after
	// the run has already been relinked to child.
	autopilotSvc.SyncRunFromTask(ctx, *parent)
	var retryCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE autopilot_run_id = $1 AND parent_task_id = $2
	`, uuidString(runID), uuidString(taskID)).Scan(&retryCount); err != nil {
		t.Fatalf("count retry tasks: %v", err)
	}
	if retryCount != 1 {
		t.Fatalf("retry task count = %d, want 1", retryCount)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'running', started_at = now()
		WHERE id = $1
	`, uuidString(child.ID)); err != nil {
		t.Fatalf("start retry task: %v", err)
	}
	if _, err := taskSvc.FailTask(ctx, child.ID, "retry exhausted no-progress failure", "", "", "codex_semantic_inactivity"); err != nil {
		t.Fatalf("FailTask retry: %v", err)
	}

	finalRun, err := queries.GetAutopilotRun(ctx, runID)
	if err != nil {
		t.Fatalf("load final run: %v", err)
	}
	if finalRun.Status != "failed" {
		t.Fatalf("final run status = %s, want failed", finalRun.Status)
	}
	if !finalRun.CompletedAt.Valid {
		t.Fatal("final run completed_at is not set")
	}
	if !finalRun.FailureReason.Valid || finalRun.FailureReason.String != "retry exhausted no-progress failure" {
		t.Fatalf("final failure_reason = %q, want retry error", finalRun.FailureReason.String)
	}

	var activeCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
	`, uuidString(agentID)).Scan(&activeCount); err != nil {
		t.Fatalf("count active tasks: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("active task count = %d, want 0", activeCount)
	}
}

func openAutopilotRetryTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unavailable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createAutopilotRetryFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (pgtype.UUID, pgtype.UUID, pgtype.UUID, pgtype.UUID, pgtype.UUID) {
	t.Helper()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("autopilot-retry-%d@example.test", suffix)
	slug := fmt.Sprintf("autopilot-retry-%d", suffix)

	var userID, workspaceID, runtimeID, agentID, autopilotID, runID, taskID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Autopilot Retry Test", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, uuidString(workspaceID))
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uuidString(userID))
	})

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'ART')
		RETURNING id
	`, "Autopilot Retry Test", slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, uuidString(workspaceID), uuidString(userID)); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Autopilot Retry Runtime', 'local', 'codex', 'online', 'test', '{}'::jsonb, now())
		RETURNING id
	`, uuidString(workspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Autopilot Retry Agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, uuidString(workspaceID), uuidString(runtimeID), uuidString(userID)).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, description, assignee_type, assignee_id,
			status, execution_mode, created_by_type, created_by_id
		)
		VALUES ($1, 'Autopilot Retry', 'test', 'agent', $2, 'active', 'run_only', 'member', $3)
		RETURNING id
	`, uuidString(workspaceID), uuidString(agentID), uuidString(userID)).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, uuidString(autopilotID)).Scan(&runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, autopilot_run_id,
			started_at, attempt, max_attempts
		)
		VALUES ($1, $2, 'running', 0, $3, now(), 1, 2)
		RETURNING id
	`, uuidString(agentID), uuidString(runtimeID), uuidString(runID)).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE autopilot_run SET task_id = $2 WHERE id = $1`, uuidString(runID), uuidString(taskID)); err != nil {
		t.Fatalf("link run task: %v", err)
	}
	return userID, workspaceID, agentID, runID, taskID
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", id.Bytes[0:4], id.Bytes[4:6], id.Bytes[6:8], id.Bytes[8:10], id.Bytes[10:16])
}
