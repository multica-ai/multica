package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestHandleFailedTasksResetsInReviewIssue is the §4.2 reverse-leak counter-example
// (MUL-4809 P1): in_review belongs to the in_progress Category, so a stuck in_review
// issue whose last task fails (no active task, no retry) must be reset to todo just
// like a literal in_progress issue. Before keying the reset off Category, the
// hardcoded `status == "in_progress"` check skipped in_review/blocked and left the
// issue stuck — this test fails on that old behavior.
func TestHandleFailedTasksResetsInReviewIssue(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)

	suffix := time.Now().UnixNano()
	var userID, workspaceID, runtimeID, agentID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('reset stuck', $1) RETURNING id`,
		fmt.Sprintf("reset-stuck-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('reset stuck', $1, '', 'RST') RETURNING id`,
		fmt.Sprintf("reset-stuck-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'reset rt', 'cloud', 'reset', 'online', 'rt', '{}'::jsonb, now(), 'private', $2) RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'reset agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3) RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Issue sitting in in_review (an in_progress-Category status), assigned to the agent.
	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		VALUES ($1, 'stuck in review', 'in_review', 'none', 'member', $2, 'agent', $3,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id::text
	`, workspaceID, userID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// One failed task for the issue, no active tasks. Empty failure_reason is not a
	// retryable reason, so MaybeRetryFailedTask is a no-op and the reset path runs.
	var taskID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority) VALUES ($1, $2, $3, 'failed', 0) RETURNING id`,
		agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert failed task: %v", err)
	}

	svc := NewTaskService(queries, pool, nil, events.New())
	task, err := queries.GetAgentTask(ctx, mustUUID(t, taskID))
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{task})

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	if status != "todo" {
		t.Fatalf("in_review issue was not reset to todo after its last task failed: status=%q", status)
	}
}
