package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestWorkflowEntryAutomationExactlyOnceReentryAndTakeover(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	suffix := time.Now().UnixNano()

	var userID, workspaceID, runtimeID, agentID, squadID, projectID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Workflow owner', $1) RETURNING id`, fmt.Sprintf("workflow-%d@multica.test", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, issue_prefix) VALUES ('Workflow automation', $1, 'LAT') RETURNING id`, fmt.Sprintf("workflow-automation-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM automation_execution WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_transition WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM squad WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'workflow-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2) RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'workflow-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb) RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'workflow-squad', '', $2, $3) RETURNING id
	`, workspaceID, agentID, userID).Scan(&squadID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, 'Workflow project') RETURNING id`, workspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := q.SeedIssueStatusEntries(ctx, workspaceID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issueworkflow.EnsureDefault(ctx, q.WithTx(tx), workspaceID); err != nil {
		t.Fatal(err)
	}
	custom, err := issueworkflow.CustomizeProject(ctx, q.WithTx(tx), workspaceID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	statuses, err := q.ListIssueWorkflowStatuses(ctx, db.ListIssueWorkflowStatusesParams{WorkspaceID: workspaceID, WorkflowID: custom.ID, IncludeArchived: false})
	if err != nil {
		t.Fatal(err)
	}
	var todo, inProgress db.IssueWorkflowStatus
	for _, status := range statuses {
		if status.LegacyStatusKey.String == "todo" {
			todo = status
		}
		if status.LegacyStatusKey.String == "in_progress" {
			inProgress = status
		}
	}
	policy := issueworkflow.EntryPolicy{
		Assignee:     issueworkflow.EntryPolicyPrincipal{Type: "agent", ID: util.UUIDToString(agentID)},
		Executor:     issueworkflow.EntryPolicyPrincipal{Type: "agent", ID: util.UUIDToString(agentID)},
		Instructions: "Implement the next workflow step.", Advance: issueworkflow.AdvanceExecutorMayTransition,
	}
	rawPolicy, _, err := issueworkflow.EncodeEntryPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue_workflow_status SET name='Ready for Agent', entry_policy=$1::jsonb, entry_policy_revision=2 WHERE id=$2`, string(rawPolicy), todo.ID); err != nil {
		t.Fatal(err)
	}
	squadPolicy := issueworkflow.EntryPolicy{
		Assignee:     issueworkflow.EntryPolicyPrincipal{Type: "squad", ID: util.UUIDToString(squadID)},
		Executor:     issueworkflow.EntryPolicyPrincipal{Type: "squad", ID: util.UUIDToString(squadID)},
		Instructions: "Coordinate the implementation as a squad.", Advance: issueworkflow.AdvanceHumanConfirms,
	}
	rawSquadPolicy, _, err := issueworkflow.EncodeEntryPolicy(squadPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue_workflow_status SET name='Squad Build', entry_policy=$1::jsonb, entry_policy_revision=2 WHERE id=$2`, string(rawSquadPolicy), inProgress.ID); err != nil {
		t.Fatal(err)
	}

	taskSvc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issueSvc := NewIssueService(q, pool, events.New(), nil, taskSvc)
	created, err := issueSvc.Create(ctx, IssueCreateParams{
		WorkspaceID: workspaceID, ProjectID: projectID, Title: "Automated issue", Status: "todo", Priority: "medium",
		CreatorType: "member", CreatorID: userID,
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Issue.AssigneeType.String != "agent" || created.Issue.AssigneeID != agentID || !created.AssignedTaskID.Valid {
		t.Fatalf("initial entry did not atomically assign and enqueue: issue=%#v task=%v", created.Issue, created.AssignedTaskID)
	}
	executions, err := q.ListIssueAutomationExecutions(ctx, db.ListIssueAutomationExecutionsParams{IssueID: created.Issue.ID, WorkspaceID: workspaceID})
	if err != nil || len(executions) != 1 || executions[0].Status != "queued" || executions[0].PolicyRevision != 2 {
		t.Fatalf("initial executions=%#v err=%v", executions, err)
	}
	var storedPolicy issueworkflow.EntryPolicy
	if err := json.Unmarshal(executions[0].PolicySnapshot, &storedPolicy); err != nil || storedPolicy.Instructions != policy.Instructions {
		t.Fatalf("policy snapshot=%s err=%v", executions[0].PolicySnapshot, err)
	}
	var linkedExecutionID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT automation_execution_id FROM agent_task_queue WHERE id=$1`, created.AssignedTaskID).Scan(&linkedExecutionID); err != nil || linkedExecutionID != executions[0].ID {
		t.Fatalf("task execution link=%v err=%v", linkedExecutionID, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, created.AssignedTaskID); err != nil {
		t.Fatal(err)
	}
	running, _ := q.GetAutomationExecution(ctx, db.GetAutomationExecutionParams{ID: executions[0].ID, WorkspaceID: workspaceID})
	if running.Status != "running" {
		t.Fatalf("execution status after task start=%q", running.Status)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed', completed_at=now() WHERE id=$1`, created.AssignedTaskID); err != nil {
		t.Fatal(err)
	}
	failed, _ := q.GetAutomationExecution(ctx, db.GetAutomationExecutionParams{ID: executions[0].ID, WorkspaceID: workspaceID})
	if failed.Status != "failed" {
		t.Fatalf("execution status after first attempt=%q", failed.Status)
	}
	retry, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{
		ID: created.AssignedTaskID, NewTaskID: dbid.NewV7(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.AutomationExecutionID != executions[0].ID {
		t.Fatalf("retry execution link=%v, want %v", retry.AutomationExecutionID, executions[0].ID)
	}
	retried, _ := q.GetAutomationExecution(ctx, db.GetAutomationExecutionParams{ID: executions[0].ID, WorkspaceID: workspaceID})
	if retried.Status != "queued" {
		t.Fatalf("execution status after retry queued=%q", retried.Status)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, retry.ID); err != nil {
		t.Fatal(err)
	}

	squadEntry, err := issueSvc.TransitionStatusNode(ctx, IssueStatusNodeTransitionParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, WorkflowStatusID: inProgress.ID,
		Actor: issueworkflow.TransitionActor{Type: "member", ID: userID}, ExpectedRevision: pgtype.Int8{Int64: created.Issue.Revision, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !squadEntry.Task.ID.Valid || squadEntry.Execution.ExecutorType.String != "squad" || squadEntry.Execution.ExecutorID != squadID || squadEntry.Issue.AssigneeType.String != "squad" || squadEntry.Issue.AssigneeID != squadID {
		t.Fatalf("squad entry did not resolve leader run and assignment: %#v", squadEntry)
	}
	if squadEntry.PreviousStatusName != "Ready for Agent" || squadEntry.StatusName != "Squad Build" {
		t.Fatalf("squad entry workflow names = %q -> %q", squadEntry.PreviousStatusName, squadEntry.StatusName)
	}
	// Both native and legacy status writes must respect the human gate and
	// leave the issue/execution untouched when an agent tries to bypass it.
	_, gateErr := issueSvc.TransitionStatusNode(ctx, IssueStatusNodeTransitionParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, WorkflowStatusID: todo.ID,
		Actor: issueworkflow.TransitionActor{Type: "agent", ID: agentID},
	})
	if !errors.Is(gateErr, ErrIssueHumanConfirmationRequired) {
		t.Fatalf("native gate error=%v", gateErr)
	}
	_, gateErr = TransitionIssue(ctx, q, pool, IssueTransitionParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, Status: "todo",
		Actor: issueworkflow.TransitionActor{Type: "system"},
	})
	if !errors.Is(gateErr, ErrIssueHumanConfirmationRequired) {
		t.Fatalf("legacy gate error=%v", gateErr)
	}
	unchanged, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: created.Issue.ID, WorkspaceID: workspaceID})
	if err != nil || unchanged.Revision != squadEntry.Issue.Revision || unchanged.WorkflowStatusID != inProgress.ID {
		t.Fatalf("rejected transition mutated the issue: %#v, %v", unchanged, err)
	}
	_, staleErr := issueSvc.TransitionStatusNode(ctx, IssueStatusNodeTransitionParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, WorkflowStatusID: todo.ID,
		Actor:                    issueworkflow.TransitionActor{Type: "member", ID: userID},
		ExpectedWorkflowRevision: pgtype.Int8{Int64: 999, Valid: true},
	})
	if !errors.Is(staleErr, ErrIssueTransitionConflict) {
		t.Fatalf("stale preview error=%v", staleErr)
	}

	reentered, err := issueSvc.TransitionStatusNode(ctx, IssueStatusNodeTransitionParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, WorkflowStatusID: todo.ID,
		Actor: issueworkflow.TransitionActor{Type: "member", ID: userID}, ExpectedRevision: pgtype.Int8{Int64: squadEntry.Issue.Revision, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reentered.Task.ID.Valid || reentered.Execution.TriggerTransitionID != reentered.Transition.ID {
		t.Fatalf("re-entry did not create a new task/execution: %#v", reentered)
	}
	if reentered.PreviousStatusName != "Squad Build" || reentered.StatusName != "Ready for Agent" {
		t.Fatalf("re-entry workflow names = %q -> %q", reentered.PreviousStatusName, reentered.StatusName)
	}
	if len(reentered.CancelledTasks) != 1 || reentered.CancelledTasks[0].ID != squadEntry.Task.ID {
		t.Fatalf("leaving squad entry did not supersede its run: %#v", reentered.CancelledTasks)
	}
	noop, err := issueSvc.TransitionStatusNode(ctx, IssueStatusNodeTransitionParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, WorkflowStatusID: todo.ID,
		Actor: issueworkflow.TransitionActor{Type: "member", ID: userID}, ExpectedRevision: pgtype.Int8{Int64: reentered.Issue.Revision, Valid: true},
	})
	if err != nil || noop.Changed {
		t.Fatalf("same-node replay=%#v err=%v", noop, err)
	}
	executions, _ = q.ListIssueAutomationExecutions(ctx, db.ListIssueAutomationExecutionsParams{IssueID: created.Issue.ID, WorkspaceID: workspaceID})
	if len(executions) != 3 {
		t.Fatalf("execution count after initial/manual/re-entry/replay=%d, want 3", len(executions))
	}
	taken, err := issueSvc.TakeOverAutomationExecution(ctx, IssueAutomationTakeoverParams{
		IssueID: created.Issue.ID, WorkspaceID: workspaceID, ExecutionID: reentered.Execution.ID,
		MemberID: userID, ExpectedRevision: pgtype.Int8{Int64: reentered.Issue.Revision, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if taken.Execution.Status != "superseded" || taken.Issue.AssigneeType.String != "member" || taken.Issue.AssigneeID != userID || len(taken.CancelledTasks) != 1 {
		t.Fatalf("takeover result=%#v", taken)
	}
}
