package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestLegacyDispatchLeavesRunIssueStatusDriven is the end-to-end enablement-boundary
// case for the dispatch path (MUL-4809 §4.1 P0-3): with the gate OFF, a create_issue
// dispatch still creates the issue and enqueues (and provenance-stamps) the task,
// but it must NOT bind the run or advance it to running — the run stays in
// issue_created for issue-status-driven finalization, exactly like the old pods a
// gate-off new pod runs alongside. The stamp lets a later gate flip bind it.
func TestLegacyDispatchLeavesRunIssueStatusDriven(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	autopilotSvc.FeatureFlags = autopilotTaskDrivenFlags(false) // legacy / rolling-deploy default

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Legacy-mode dispatch",
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: "Legacy issue", Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID) })

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("legacy dispatch did not create an issue: %+v", run)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, run.IssueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, run.IssueID)
	})

	// Legacy: run stays issue_created and unbound.
	if run.Status != "issue_created" {
		t.Fatalf("legacy dispatch advanced the run: status=%q, want issue_created", run.Status)
	}
	if run.TaskID.Valid {
		t.Fatalf("legacy dispatch bound the run's task_id; it must stay unbound in legacy mode")
	}

	// The dispatched task exists and carries the run's provenance stamp.
	tasks, err := queries.ListTasksByIssue(ctx, run.IssueID)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one dispatched task, got %d", len(tasks))
	}
	if !tasks[0].DispatchedAutopilotRunID.Valid || tasks[0].DispatchedAutopilotRunID.Bytes != run.ID.Bytes {
		t.Fatal("legacy-dispatched task not provenance-stamped; a later gate flip could not bind it")
	}
}
