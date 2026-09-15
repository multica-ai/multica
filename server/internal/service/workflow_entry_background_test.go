package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func backgroundEntryProject(t *testing.T, fx principalFixture, executorID string) (string, string) {
	t.Helper()
	projectID := fx.Project(t, "Background entry")
	workflowID := fx.Insert(t, "issue_workflow", testutil.Cols{
		"workspace_id": fx.WorkspaceID, "scope_type": "project", "scope_id": projectID, "name": "Background entry",
	})
	statusID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": fx.WorkspaceID, "workflow_id": workflowID, "name": "Build", "spec_key": "build", "phase": "started", "color": "#123456", "position": 0,
		"entry_policy": `{"executor":{"type":"agent","id":"` + executorID + `"},"instructions":"Build the requested change."}`,
	})
	fx.Exec(t, `UPDATE issue_workflow SET initial_status_id=$1 WHERE id=$2`, statusID, workflowID)
	fx.Exec(t, `UPDATE project SET default_issue_workflow_id=$1 WHERE id=$2`, workflowID, projectID)
	fx.Cleanup(t, `DELETE FROM issue WHERE workspace_id=$1`, fx.WorkspaceID)
	fx.Cleanup(t, `DELETE FROM issue_transition WHERE workspace_id=$1`, fx.WorkspaceID)
	fx.Cleanup(t, `DELETE FROM automation_execution WHERE workspace_id=$1`, fx.WorkspaceID)
	fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id=$1)`, fx.WorkspaceID)
	return projectID, statusID
}

func TestWorkflowEntryFailureDoesNotReenterBusinessStatus(t *testing.T) {
	ctx := context.Background()
	fx, ownerID := newPrincipalFixture(t)
	executorID := fx.privateAgentOwnedBy(t, ownerID, "entry-failure")
	projectID, statusID := backgroundEntryProject(t, fx, executorID)
	var workflowID string
	fx.QueryRow(t, `SELECT workflow_id FROM issue_workflow_status WHERE id=$1`, statusID).Scan(&workflowID)
	// A valid automated Todo makes an accidental failure-recovery reset
	// observable: it would create another transition and start this action.
	fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": fx.WorkspaceID, "workflow_id": workflowID, "name": "Todo", "spec_key": "todo", "legacy_status_key": "todo", "phase": "unstarted", "color": "#123456", "position": 1,
		"entry_policy": `{"executor":{"type":"agent","id":"` + executorID + `"},"instructions":"Start another attempt."}`,
	})
	svc := NewIssueService(fx.q, fx.Pool, fx.svc.Bus, nil, fx.svc.TaskSvc)
	created, err := svc.Create(ctx, IssueCreateParams{
		WorkspaceID: util.MustParseUUID(fx.WorkspaceID), ProjectID: util.MustParseUUID(projectID),
		Title: "Failure preserves entry", Priority: "none", CreatorType: "member", CreatorID: util.MustParseUUID(ownerID),
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	fx.Exec(t, `UPDATE agent_task_queue SET status='failed', failure_reason='agent_error', error='test failure', attempt=1, max_attempts=1, completed_at=now() WHERE id=$1`, created.AssignedTaskID)
	failed, err := fx.q.GetAgentTask(ctx, created.AssignedTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if retried := fx.svc.TaskSvc.HandleFailedTasks(ctx, []db.AgentTaskQueue{failed}); retried != 0 {
		t.Fatalf("terminal failure unexpectedly retried %d tasks", retried)
	}
	current, err := fx.q.GetIssue(ctx, created.Issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkflowStatusID != util.MustParseUUID(statusID) || current.Revision != created.Issue.Revision || current.LastTransitionID != created.Issue.LastTransitionID {
		t.Fatalf("execution failure changed the business status: %#v", current)
	}
	if fx.Count(t, `SELECT count(*) FROM automation_execution WHERE issue_id=$1 AND status='failed'`, current.ID) != 1 ||
		fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, current.ID) != 1 {
		t.Fatal("terminal failure must retain the same failed execution without another entry task")
	}
}

func TestAutopilotCreationAppliesWorkflowEntry(t *testing.T) {
	for _, mode := range []string{"automated", "manual", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			fx, ownerID := newPrincipalFixture(t)
			triggerOwnerID := fx.member(t, "entry-trigger-owner")
			assigneeID := fx.privateAgentOwnedBy(t, triggerOwnerID, "entry-assignee")
			executorID := fx.privateAgentOwnedBy(t, ownerID, "entry-executor")
			projectID, statusID := backgroundEntryProject(t, fx, executorID)
			apID, triggerID := fx.autopilotWithTrigger(t, assigneeID, ownerID, triggerOwnerID)
			fx.Exec(t, `UPDATE autopilot SET execution_mode='create_issue', project_id=$1 WHERE id=$2`, projectID, apID)
			fx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id=$1`, apID)
			if mode == "manual" {
				fx.Exec(t, `UPDATE issue_workflow_status SET entry_policy='{}' WHERE id=$1`, statusID)
			} else if mode == "unavailable" {
				fx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, executorID)
			}
			ap, err := fx.q.GetAutopilot(ctx, util.MustParseUUID(apID))
			if err != nil {
				t.Fatal(err)
			}
			run, err := fx.svc.DispatchAutopilot(ctx, ap, util.MustParseUUID(triggerID), "schedule", nil)
			if mode == "unavailable" {
				if err == nil || fx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id=$1`, fx.WorkspaceID) != 0 ||
					fx.Count(t, `SELECT count(*) FROM issue_transition WHERE workspace_id=$1`, fx.WorkspaceID) != 0 ||
					fx.Count(t, `SELECT count(*) FROM automation_execution WHERE workspace_id=$1`, fx.WorkspaceID) != 0 {
					t.Fatalf("unavailable executor did not roll back issue creation: %v", err)
				}
				return
			}
			if err != nil || run == nil || !run.IssueID.Valid {
				t.Fatalf("dispatch: run=%#v err=%v", run, err)
			}
			if err := fx.svc.ensureWebhookCreateIssueTask(ctx, ap, *run); err != nil {
				t.Fatal(err)
			}
			tasks, err := fx.q.ListTasksByIssue(ctx, run.IssueID)
			if err != nil {
				t.Fatal(err)
			}
			wantTasks := 1
			if mode == "manual" {
				wantTasks = 0
			}
			if len(tasks) != wantTasks {
				t.Fatalf("entry/recovery queued %d tasks, want %d", len(tasks), wantTasks)
			}
			if mode == "automated" {
				task := tasks[0]
				if task.AgentID != util.MustParseUUID(executorID) || !task.AutomationExecutionID.Valid || task.OriginatorUserID != util.MustParseUUID(triggerOwnerID) || task.OriginatorSource.String != "trigger_owner" {
					t.Fatalf("entry lost its executor or trigger principal: %#v", task)
				}
			}
			if fx.Count(t, `SELECT count(*) FROM issue_transition WHERE issue_id=$1 AND to_status_id=$2`, run.IssueID, statusID) != 1 {
				t.Fatal("initial entry must record one transition")
			}
		})
	}
}

func TestAutopilotWorkflowAgentHandoffPreservesPrincipal(t *testing.T) {
	for _, source := range []string{"manual", "schedule"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			fx, ownerID := newPrincipalFixture(t)
			principalID := fx.member(t, "workflow-handoff-principal")
			assigneeID := fx.privateAgentOwnedBy(t, principalID, "handoff-assignee")
			executorID := fx.privateAgentOwnedBy(t, ownerID, "handoff-builder")
			reviewerID := fx.privateAgentOwnedBy(t, ownerID, "handoff-reviewer")
			projectID, statusID := backgroundEntryProject(t, fx, executorID)
			var workflowID string
			fx.QueryRow(t, `SELECT workflow_id FROM issue_workflow_status WHERE id=$1`, statusID).Scan(&workflowID)
			reviewID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
				"workspace_id": fx.WorkspaceID, "workflow_id": workflowID, "name": "Review", "spec_key": "review", "phase": "started", "color": "#123456", "position": 1,
				"entry_policy": `{"executor":{"type":"agent","id":"` + reviewerID + `"},"instructions":"Review the change."}`,
			})
			apID, triggerID := fx.autopilotWithTrigger(t, assigneeID, ownerID, principalID)
			fx.Exec(t, `UPDATE autopilot SET execution_mode='create_issue', project_id=$1 WHERE id=$2`, projectID, apID)
			fx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id=$1`, apID)
			ap, err := fx.q.GetAutopilot(ctx, util.MustParseUUID(apID))
			if err != nil {
				t.Fatal(err)
			}
			var run *db.AutopilotRun
			if source == "manual" {
				run, _, err = fx.svc.DispatchAutopilotManual(ctx, ap, pgtype.UUID{}, nil, util.MustParseUUID(principalID))
			} else {
				run, err = fx.svc.DispatchAutopilot(ctx, ap, util.MustParseUUID(triggerID), source, nil)
			}
			if err != nil || run == nil || !run.IssueID.Valid {
				t.Fatalf("dispatch: run=%#v err=%v", run, err)
			}
			tasks, err := fx.q.ListTasksByIssue(ctx, run.IssueID)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("initial tasks: %#v err=%v", tasks, err)
			}
			parent := tasks[0]
			fx.Exec(t, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, parent.ID)
			svc := NewIssueService(fx.q, fx.Pool, fx.svc.Bus, nil, fx.svc.TaskSvc)
			params := IssueStatusNodeTransitionParams{
				IssueID: run.IssueID, WorkspaceID: util.MustParseUUID(fx.WorkspaceID), WorkflowStatusID: util.MustParseUUID(reviewID),
				Actor: issueworkflow.TransitionActor{Type: "agent", ID: parent.AgentID, TaskID: parent.ID},
			}
			result, err := svc.TransitionStatusNode(ctx, params)
			if err != nil {
				t.Fatalf("agent handoff failed: %v", err)
			}
			if !result.Changed || result.Task.AgentID != util.MustParseUUID(reviewerID) ||
				result.Task.OriginatorUserID != util.MustParseUUID(principalID) || result.Task.AccountableUserID != util.MustParseUUID(principalID) ||
				result.Task.OriginatorSource.String != "delegation" || result.Task.DelegatedFromTaskID != parent.ID {
				t.Fatalf("handoff lost the initiating human or parent task: %#v", result.Task)
			}
			parentAfter, err := fx.q.GetAgentTask(ctx, parent.ID)
			if err != nil || parentAfter.Status != "running" {
				t.Fatalf("self handoff cancelled its own run: %#v err=%v", parentAfter, err)
			}
			params.Actor = issueworkflow.TransitionActor{Type: "member", ID: util.MustParseUUID(principalID)}
			repeated, err := svc.TransitionStatusNode(ctx, params)
			if err != nil || repeated.Changed || fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, run.IssueID) != 2 {
				t.Fatalf("repeating the current node created another task: %#v err=%v", repeated, err)
			}
		})
	}
}
