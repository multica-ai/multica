package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setupWorkflowNodeRunFixture creates a minimal workflow graph and returns
// the workflow_node_run id. The issue/agent/runtime fixture is reused from
// the rerun tests.
func setupWorkflowNodeRunFixture(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var workflowID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow (workspace_id, title, max_retries, created_by_type, created_by_id)
		VALUES ($1, 'Retry workflow test', 3, 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&workflowID); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, workflowID)
	})

	var nodeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node (
			workflow_id, title, worker_type, worker_id, critic_type, critic_id
		) VALUES ($1, 'Test node', 'agent', NULL, 'agent', NULL)
		RETURNING id
	`, workflowID).Scan(&nodeID); err != nil {
		t.Fatalf("create workflow node: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_run (
			workflow_id, workspace_id, workflow_title, status, triggered_by_type, triggered_by_id
		) VALUES ($1, $2, 'Retry run', 'running', 'member', $3)
		RETURNING id
	`, workflowID, testWorkspaceID, testUserID).Scan(&runID); err != nil {
		t.Fatalf("create workflow run: %v", err)
	}

	var nodeRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node_run (
			workflow_run_id, workflow_node_id, node_title, status,
			worker_type, worker_id, critic_type, critic_id
		) VALUES ($1, $2, 'Test node run', 'working', 'agent', NULL, 'agent', NULL)
		RETURNING id
	`, runID, nodeID).Scan(&nodeRunID); err != nil {
		t.Fatalf("create workflow node run: %v", err)
	}

	return nodeRunID
}

// TestCreateRetryTaskPreservesWorkflowNodeRunID asserts that an auto-retry
// clones the workflow_node_run_id link. If this link is dropped, the retry
// task's completion is ignored by the workflow state machine and the run
// stays stuck on the previously failed node.
func TestCreateRetryTaskPreservesWorkflowNodeRunID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	nodeRunID := setupWorkflowNodeRunFixture(t)

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			started_at, completed_at, failure_reason,
			attempt, max_attempts, workflow_node_run_id
		) VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error',
		        1, 3, $4)
		RETURNING id
	`, agentID, runtimeID, issueID, nodeRunID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryTask(ctx, pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}

	if !child.WorkflowNodeRunID.Valid {
		t.Fatal("expected retry child to preserve workflow_node_run_id, got NULL")
	}
	if got := util.UUIDToString(child.WorkflowNodeRunID); got != nodeRunID {
		t.Fatalf("workflow_node_run_id mismatch: got %s, want %s", got, nodeRunID)
	}
}

// TestRerunIssuePreservesWorkflowNodeRunID asserts that a manual rerun of a
// workflow-linked task keeps the workflow_node_run_id link. Without this, the
// rerun can succeed but the workflow state machine never notices.
func TestRerunIssuePreservesWorkflowNodeRunID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	nodeRunID := setupWorkflowNodeRunFixture(t)

	ctx := context.Background()

	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			started_at, completed_at, failure_reason,
			workflow_node_run_id
		) VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error',
		        $4)
		RETURNING id
	`, agentID, runtimeID, issueID, nodeRunID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceTaskID), Valid: true},
		pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}
	if !task.WorkflowNodeRunID.Valid {
		t.Fatal("expected rerun task to preserve workflow_node_run_id, got NULL")
	}
	if got := util.UUIDToString(task.WorkflowNodeRunID); got != nodeRunID {
		t.Fatalf("workflow_node_run_id mismatch: got %s, want %s", got, nodeRunID)
	}
}
