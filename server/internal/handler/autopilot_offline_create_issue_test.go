package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotCreateIssueOfflineRuntimeQueuesForLaterClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	suffix := time.Now().UnixNano()
	runtimeName := fmt.Sprintf("offline-create-issue-runtime-%d", suffix)
	agentName := fmt.Sprintf("offline-create-issue-agent-%d", suffix)
	autopilotTitle := fmt.Sprintf("offline create_issue autopilot %d", suffix)

	var runtimeID, agentID, autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'offline', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, runtimeName, runtimeName, "offline create_issue runtime", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, agentName, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create offline-runtime agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, description, assignee_type, assignee_id, status,
			execution_mode, issue_title_template, created_by_type, created_by_id
		)
		VALUES ($1, $2, 'daily report', 'agent', $3, 'active',
			'create_issue', 'Daily report {{date}}', 'member', $4)
		RETURNING id
	`, testWorkspaceID, autopilotTitle, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id IN (SELECT id FROM issue WHERE origin_id = $1)`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM issue_subscriber WHERE issue_id IN (SELECT id FROM issue WHERE origin_id = $1)`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE origin_id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("DispatchAutopilot returned nil run")
	}
	if run.Status != "issue_created" {
		t.Fatalf("run status = %q, want issue_created", run.Status)
	}
	if !run.IssueID.Valid {
		t.Fatalf("run issue_id invalid; run = %+v", run)
	}
	if run.FailureReason.Valid {
		t.Fatalf("failure_reason = %q, want empty", run.FailureReason.String)
	}

	issueID := uuidToString(run.IssueID)
	var issueTitle, issueStatus, assigneeID string
	if err := testPool.QueryRow(ctx, `
		SELECT title, status, assignee_id::text
		FROM issue
		WHERE id = $1 AND origin_type = 'autopilot' AND origin_id = $2
	`, issueID, autopilotID).Scan(&issueTitle, &issueStatus, &assigneeID); err != nil {
		t.Fatalf("load created issue: %v", err)
	}
	if issueTitle == "" || issueStatus != "todo" || assigneeID != agentID {
		t.Fatalf("created issue title/status/assignee = %q/%q/%q, want non-empty/todo/%q",
			issueTitle, issueStatus, assigneeID, agentID)
	}

	var taskID, taskStatus, taskRuntimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text, status, runtime_id::text
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issueID, agentID).Scan(&taskID, &taskStatus, &taskRuntimeID); err != nil {
		t.Fatalf("load queued task: %v", err)
	}
	if taskStatus != "queued" || taskRuntimeID != runtimeID {
		t.Fatalf("task status/runtime = %q/%q, want queued/%q", taskStatus, taskRuntimeID, runtimeID)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("bring runtime online: %v", err)
	}
	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("ClaimTaskForRuntime: %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimTaskForRuntime returned no task")
	}
	if got := uuidToString(claimed.ID); got != taskID {
		t.Fatalf("claimed task = %s, want %s", got, taskID)
	}
	if claimed.Status != "dispatched" {
		t.Fatalf("claimed task status = %q, want dispatched", claimed.Status)
	}
}
