package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotRunOnlyDispatchRejectedBeforeTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Run-only guardrail regression",
		Description:        pgtype.Text{String: "Legacy run-only issue-bound prompt", Valid: true},
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err == nil {
		t.Fatal("expected run_only dispatch to fail before task creation")
	}
	if !strings.Contains(err.Error(), "issue-bound dispatch requires issue_id") {
		t.Fatalf("expected explicit issue_id error, got %v", err)
	}
	if run == nil {
		t.Fatal("expected failed autopilot run to be returned")
	}
	if run.TaskID.Valid {
		t.Fatalf("run_only guardrail should not create task, got task_id=%v", run.TaskID)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1`,
		run.ID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count task rows: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no agent tasks for rejected run_only dispatch, got %d", taskCount)
	}
}

func TestAutopilotCreateIssueDispatchLinksIssueBeforeTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Create issue guardrail regression",
		Description:        pgtype.Text{String: "Create issue before dispatch", Valid: true},
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: "Guardrail issue {{date}}", Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot create_issue: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("create_issue dispatch must return a run with issue_id, got %#v", run)
	}

	var taskIssueID string
	if err := testPool.QueryRow(ctx,
		`SELECT issue_id::text FROM agent_task_queue WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`,
		run.IssueID,
	).Scan(&taskIssueID); err != nil {
		t.Fatalf("load queued task for created issue: %v", err)
	}
	if taskIssueID == "" {
		t.Fatal("create_issue dispatch queued a task without issue_id")
	}
}
