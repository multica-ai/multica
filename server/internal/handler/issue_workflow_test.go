package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectWorkflowAPIAndStatusNodeTransition(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workflow bootstrap: %v", err)
	}
	workspaceWorkflow, err := issueworkflow.EnsureDefault(ctx, testHandler.Queries.WithTx(tx), workspaceID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure workspace workflow: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit workflow bootstrap: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, workspaceID, "Workflow API "+time.Now().Format("150405.000000000")).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	var destinationProjectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, workspaceID, "Workflow move target "+time.Now().Format("150405.000000000")).Scan(&destinationProjectID); err != nil {
		t.Fatalf("create destination project: %v", err)
	}
	t.Cleanup(func() {
		var customWorkflowID pgtype.UUID
		_ = testPool.QueryRow(context.Background(), `
			SELECT id FROM issue_workflow
			WHERE workspace_id = $1 AND scope_type = 'project' AND scope_id = $2
		`, workspaceID, projectID).Scan(&customWorkflowID)
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM automation_execution WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)
		`, projectID)
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM issue_transition WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)
		`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id IN ($1, $2)`, projectID, destinationProjectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id IN ($1, $2)`, projectID, destinationProjectID)
		if customWorkflowID.Valid {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workflow_id = $1`, customWorkflowID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, customWorkflowID)
		}
	})

	var customized issueWorkflowResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom",
		}), "id", projectID)).Want(http.StatusOK).JSON(&customized)
	if customized.Mode != "custom" || customized.Workflow.ScopeType != "project" || customized.Workflow.ScopeID != projectID {
		t.Fatalf("custom workflow response = %#v", customized)
	}
	if len(customized.Statuses) < 7 {
		t.Fatalf("custom workflow status count = %d, want at least 7", len(customized.Statuses))
	}

	var inProgressID, backlogID string
	for _, status := range customized.Statuses {
		if status.LegacyStatusKey == nil {
			continue
		}
		switch *status.LegacyStatusKey {
		case "in_progress":
			inProgressID = status.ID
		case "backlog":
			backlogID = status.ID
		}
	}
	if inProgressID == "" || backlogID == "" {
		t.Fatalf("custom workflow is missing required status nodes: %#v", customized.Statuses)
	}

	for _, nextKey := range []string{"missing_stage", "in_progress"} {
		testutil.Call(t, testHandler.UpdateIssueWorkflowStatus,
			testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+inProgressID, map[string]any{
				"expected_revision": customized.Workflow.Revision,
				"entry_policy":      map[string]any{"next_status_key": nextKey},
			}), "workflowId", customized.Workflow.ID, "statusId", inProgressID)).Want(http.StatusBadRequest)
	}

	// Definition mutations advance the workflow version once. Entry Policy
	// has its own revision so a worker can pin the exact instructions it ran.
	definitionRevision := customized.Workflow.Revision
	var updated issueWorkflowResponse
	updateBody := func(revision int64) map[string]any {
		return map[string]any{
			"expected_revision": revision,
			"name":              "Building",
			"entry_policy": map[string]any{
				"assignee":        map[string]any{"type": "human", "id": testUserID},
				"executor":        map[string]any{"type": "none"},
				"instructions":    "Wait for a human confirmation.",
				"advance":         "human_confirms",
				"next_status_key": "todo",
			},
		}
	}
	testutil.Call(t, testHandler.UpdateIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+inProgressID, updateBody(definitionRevision)),
			"workflowId", customized.Workflow.ID, "statusId", inProgressID)).Want(http.StatusOK).JSON(&updated)
	if updated.Workflow.Revision != definitionRevision+1 {
		t.Fatalf("updated workflow revision = %d, want %d", updated.Workflow.Revision, definitionRevision+1)
	}
	var updatedNode issueWorkflowStatusResponse
	for _, status := range updated.Statuses {
		if status.ID == inProgressID {
			updatedNode = status
			break
		}
	}
	if updatedNode.Name != "Building" || updatedNode.EntryPolicyRevision != 2 ||
		updatedNode.EntryPolicy.Assignee.Type != issueworkflow.AssigneeHuman ||
		updatedNode.EntryPolicy.Assignee.ID != testUserID || updatedNode.EntryPolicy.NextStatusKey != "todo" {
		t.Fatalf("updated workflow node = %#v", updatedNode)
	}

	// Repeating the same semantic policy is a stable no-op for both versions.
	var noopDefinition issueWorkflowResponse
	testutil.Call(t, testHandler.UpdateIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+inProgressID, updateBody(updated.Workflow.Revision)),
			"workflowId", customized.Workflow.ID, "statusId", inProgressID)).Want(http.StatusOK).JSON(&noopDefinition)
	if noopDefinition.Workflow.Revision != updated.Workflow.Revision {
		t.Fatalf("no-op workflow revision = %d, want %d", noopDefinition.Workflow.Revision, updated.Workflow.Revision)
	}
	testutil.Call(t, testHandler.UpdateIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+inProgressID, updateBody(definitionRevision)),
			"workflowId", customized.Workflow.ID, "statusId", inProgressID)).Want(http.StatusConflict)

	testutil.Call(t, testHandler.UpdateIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+inProgressID, map[string]any{
			"expected_revision": updated.Workflow.Revision,
			"entry_policy": map[string]any{
				"assignee": map[string]any{"type": "keep"}, "executor": map[string]any{"type": "none"},
				"instructions": "", "advance": "executor_may_transition",
			},
		}), "workflowId", customized.Workflow.ID, "statusId", inProgressID)).Want(http.StatusBadRequest)

	// Reordering replaces the complete active order atomically.
	statusIDs := make([]string, len(updated.Statuses))
	for i := range updated.Statuses {
		statusIDs[len(updated.Statuses)-1-i] = updated.Statuses[i].ID
	}
	var reordered issueWorkflowResponse
	testutil.Call(t, testHandler.ReorderIssueWorkflowStatuses,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/reorder", map[string]any{
			"expected_revision": updated.Workflow.Revision, "status_ids": statusIDs,
		}), "workflowId", customized.Workflow.ID)).Want(http.StatusOK).JSON(&reordered)
	if reordered.Workflow.Revision != updated.Workflow.Revision+1 || reordered.Statuses[0].ID != statusIDs[0] {
		t.Fatalf("reordered workflow = %#v", reordered)
	}

	// Archive retains the node in definition history but prevents future use.
	if reordered.Workflow.InitialStatusID == nil {
		t.Fatal("custom workflow is missing its initial status")
	}
	testutil.Call(t, testHandler.ArchiveIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+*reordered.Workflow.InitialStatusID+"?expected_revision="+strconv.FormatInt(reordered.Workflow.Revision, 10), nil),
			"workflowId", customized.Workflow.ID, "statusId", *reordered.Workflow.InitialStatusID)).Want(http.StatusConflict)

	var archived issueWorkflowResponse
	testutil.Call(t, testHandler.ArchiveIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+backlogID+"?expected_revision="+strconv.FormatInt(reordered.Workflow.Revision, 10), nil),
			"workflowId", customized.Workflow.ID, "statusId", backlogID)).Want(http.StatusOK).JSON(&archived)
	if archived.Workflow.Revision != reordered.Workflow.Revision+1 {
		t.Fatalf("archived workflow revision = %d, want %d", archived.Workflow.Revision, reordered.Workflow.Revision+1)
	}
	var archivedAt *string
	for _, status := range archived.Statuses {
		if status.ID == backlogID {
			archivedAt = status.ArchivedAt
		}
	}
	if archivedAt == nil {
		t.Fatal("archived status is missing archived_at")
	}
	testutil.Call(t, testHandler.UpdateIssueWorkflowStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-workflows/"+customized.Workflow.ID+"/statuses/"+backlogID, map[string]any{"expected_revision": archived.Workflow.Revision, "name": "Later"}),
			"workflowId", customized.Workflow.ID, "statusId", backlogID)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "archived workflow status", "status": "backlog", "project_id": projectID,
		})).Want(http.StatusConflict)
	customized = archived

	var effective issueWorkflowResponse
	testutil.Call(t, testHandler.GetEffectiveIssueWorkflow,
		newRequest(http.MethodGet, "/api/issue-workflows/effective?project_id="+projectID, nil)).Want(http.StatusOK).JSON(&effective)
	if effective.Workflow.ID != customized.Workflow.ID || effective.Mode != "custom" {
		t.Fatalf("effective workflow response = %#v", effective)
	}
	var concrete issueWorkflowResponse
	testutil.Call(t, testHandler.GetIssueWorkflow,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/issue-workflows/"+customized.Workflow.ID, nil),
			"workflowId", customized.Workflow.ID)).Want(http.StatusOK).JSON(&concrete)
	if concrete.Workflow.ID != customized.Workflow.ID || len(concrete.Statuses) != len(customized.Statuses) {
		t.Fatalf("concrete workflow response = %#v", concrete)
	}

	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "workflow API issue", "status": "todo", "project_id": projectID,
		})).Want(http.StatusCreated).JSON(&created)
	if created.WorkflowID == nil || *created.WorkflowID != customized.Workflow.ID || created.WorkflowStatusID == nil {
		t.Fatalf("created issue workflow binding = %#v", created)
	}
	var listed struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListIssues,
		newRequest(http.MethodGet, "/api/issues?project_id="+projectID+"&limit=100", nil)).Want(http.StatusOK).JSON(&listed)
	var listedCreated *IssueResponse
	for i := range listed.Issues {
		if listed.Issues[i].ID == created.ID {
			listedCreated = &listed.Issues[i]
			break
		}
	}
	if listedCreated == nil || listedCreated.WorkflowID == nil || *listedCreated.WorkflowID != customized.Workflow.ID || listedCreated.WorkflowStatusID == nil || listedCreated.TransitionID == nil {
		t.Fatalf("list response omitted canonical workflow cursors: %#v", listedCreated)
	}

	var transitioned transitionIssueStatusNodeResponse
	testutil.Call(t, testHandler.TransitionIssueStatusNode,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/transitions", map[string]any{
			"workflow_status_id":     inProgressID,
			"expected_revision":      created.Revision,
			"expected_transition_id": created.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&transitioned)
	if transitioned.Issue.Status != "in_progress" || transitioned.Issue.WorkflowStatusID == nil || *transitioned.Issue.WorkflowStatusID != inProgressID {
		t.Fatalf("status-node transition response = %#v", transitioned)
	}
	if transitioned.Transition == nil || transitioned.Transition.ID == "" || transitioned.Transition.ToStatusID != inProgressID {
		t.Fatalf("transition audit response = %#v", transitioned.Transition)
	}
	if transitioned.Execution == nil || transitioned.Execution.Status != "dormant" || transitioned.Execution.TriggerTransitionID != transitioned.Transition.ID || transitioned.TaskID != nil {
		t.Fatalf("manual entry execution response = %#v", transitioned)
	}
	// Generic edits (including drag and batch updates) use this locked write
	// boundary too; rejecting the gate must roll back before any mutation.
	_, _, _, gateErr := testHandler.updateIssueAtomically(ctx, workspaceID, db.UpdateIssueParams{
		ID:     parseUUID(created.ID),
		Status: pgtype.Text{String: "todo", Valid: true},
	}, map[string]json.RawMessage{"status": json.RawMessage(`"todo"`)}, nil, nil, nil, "todo", nil,
		issueworkflow.TransitionActor{Type: "agent"}, "issue_updated")
	if !errors.Is(gateErr, service.ErrIssueHumanConfirmationRequired) {
		t.Fatalf("generic status gate error=%v", gateErr)
	}
	stillReview, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil || stillReview.Revision != transitioned.Issue.Revision || stillReview.Status != "in_progress" {
		t.Fatalf("rejected generic edit changed issue: %#v, %v", stillReview, err)
	}

	var executionHistory []automationExecutionResponse
	testutil.Call(t, testHandler.ListIssueAutomationExecutions,
		withURLParam(newRequest(http.MethodGet, "/api/issues/"+created.ID+"/automation-executions", nil), "id", created.ID)).Want(http.StatusOK).JSON(&executionHistory)
	if len(executionHistory) != 2 || executionHistory[0].ID != transitioned.Execution.ID {
		t.Fatalf("automation execution history = %#v", executionHistory)
	}
	var noop transitionIssueStatusNodeResponse
	testutil.Call(t, testHandler.TransitionIssueStatusNode,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/transitions", map[string]any{
			"workflow_status_id":     inProgressID,
			"expected_revision":      transitioned.Issue.Revision,
			"expected_transition_id": transitioned.Issue.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&noop)
	if noop.Transition != nil || noop.Execution != nil || noop.TaskID != nil || noop.Issue.Revision != transitioned.Issue.Revision {
		t.Fatalf("same-node transition should be a stable no-op: %#v", noop)
	}

	var inherited issueWorkflowResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "default",
		}), "id", projectID)).Want(http.StatusOK).JSON(&inherited)
	if inherited.Mode != "default" || inherited.Workflow.ID != uuidToString(workspaceWorkflow.ID) {
		t.Fatalf("default workflow response = %#v", inherited)
	}
	pinned, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("reload pinned issue: %v", err)
	}
	if uuidToString(pinned.WorkflowID) != customized.Workflow.ID {
		t.Fatalf("switching project default drifted existing issue workflow to %s", uuidToString(pinned.WorkflowID))
	}

	// A project move that crosses workflow definitions cannot guess a mapping
	// from matching names or legacy keys. The client must choose a target node.
	testutil.Call(t, testHandler.UpdateIssue,
		withURLParam(newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
			"project_id": destinationProjectID,
		}), "id", created.ID)).Want(http.StatusConflict)
	workspaceInProgress, err := testHandler.Queries.GetIssueWorkflowStatusByLegacyKey(ctx, db.GetIssueWorkflowStatusByLegacyKeyParams{
		WorkspaceID: workspaceID, WorkflowID: workspaceWorkflow.ID,
		LegacyStatusKey: pgtype.Text{String: "in_progress", Valid: true},
	})
	if err != nil {
		t.Fatalf("load workspace in-progress node: %v", err)
	}
	var moved IssueResponse
	testutil.Call(t, testHandler.UpdateIssue,
		withURLParam(newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
			"project_id":             destinationProjectID,
			"workflow_status_id":     uuidToString(workspaceInProgress.ID),
			"expected_revision":      transitioned.Issue.Revision,
			"expected_transition_id": transitioned.Issue.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&moved)
	if moved.ProjectID == nil || *moved.ProjectID != destinationProjectID || moved.WorkflowID == nil || *moved.WorkflowID != uuidToString(workspaceWorkflow.ID) || moved.WorkflowStatusID == nil || *moved.WorkflowStatusID != uuidToString(workspaceInProgress.ID) {
		t.Fatalf("explicit cross-workflow move = %#v", moved)
	}

	reloaded, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("reload existing issue: %v", err)
	}
	if uuidToString(reloaded.WorkflowID) != uuidToString(workspaceWorkflow.ID) {
		t.Fatalf("cross-workflow move did not bind target workflow: %s", uuidToString(reloaded.WorkflowID))
	}

	// The storage row is still a stable status-node binding, not merely the
	// compatibility key asserted above.
	status, err := testHandler.Queries.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: workspaceID, WorkflowID: reloaded.WorkflowID, ID: reloaded.WorkflowStatusID,
	})
	if err != nil || uuidToString(status.ID) != uuidToString(workspaceInProgress.ID) {
		t.Fatalf("load canonical status node = %#v, err=%v", status, err)
	}
}

func TestProjectWorkflowSpecApplyIsDeclarativeAndWorkflowNative(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workspaceWorkflow, err := issueworkflow.EnsureDefault(ctx, testHandler.Queries.WithTx(tx), workspaceID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure workspace workflow: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, workspaceID, "Workflow spec "+time.Now().Format("150405.000000000")).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	var workflowID string
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM automation_execution WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_transition WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		if workflowID != "" {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workflow_id = $1`, workflowID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, workflowID)
		}
	})

	status := func(key, name, phase, color string) map[string]any {
		return map[string]any{
			"key": key, "name": name, "phase": phase, "color": color,
			"entry_policy": map[string]any{
				"assignee": map[string]any{"type": "keep"}, "executor": map[string]any{"type": "none"},
				"advance": "human_confirms",
			},
		}
	}
	spec := map[string]any{
		"api_version": 1, "name": "SDLC", "initial_status": "technical_spec",
		"statuses": []map[string]any{
			status("technical_spec", "Technical Spec", "unstarted", "#8b5cf6"),
			status("implementation", "Implementation", "started", "#2563eb"),
			status("shipped", "Shipped", "completed", "#16a34a"),
		},
	}
	var preview issueWorkflowApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom", "spec": spec, "dry_run": true, "expected_revision": workspaceWorkflow.Revision,
		}), "id", projectID)).Want(http.StatusOK).JSON(&preview)
	if !preview.DryRun || !preview.Plan.Changed || len(preview.Plan.Created) != 3 {
		t.Fatalf("dry-run preview = %#v", preview)
	}
	var customCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_workflow WHERE workspace_id = $1 AND scope_type = 'project' AND scope_id = $2`, workspaceID, projectID).Scan(&customCount); err != nil {
		t.Fatal(err)
	}
	if customCount != 0 {
		t.Fatalf("dry-run persisted %d project workflow rows", customCount)
	}
	var applied issueWorkflowApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom", "spec": spec,
		}), "id", projectID)).Want(http.StatusOK).JSON(&applied)
	workflowID = applied.Workflow.ID
	if !applied.Plan.Changed || len(applied.Plan.Created) != 3 || applied.Workflow.InitialStatusID == nil {
		t.Fatalf("first apply = %#v", applied)
	}
	var initialID, implementationID string
	for _, node := range applied.Statuses {
		if node.LegacyStatusKey != nil {
			t.Fatalf("new spec node %q unexpectedly has legacy key %q", node.SpecKey, *node.LegacyStatusKey)
		}
		switch node.SpecKey {
		case "technical_spec":
			initialID = node.ID
		case "implementation":
			implementationID = node.ID
		}
	}
	if initialID == "" || implementationID == "" || *applied.Workflow.InitialStatusID != initialID {
		t.Fatalf("initial status binding = %#v", applied)
	}

	var replay issueWorkflowApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom", "spec": spec, "expected_revision": applied.Workflow.Revision,
		}), "id", projectID)).Want(http.StatusOK).JSON(&replay)
	if replay.Plan.Changed || replay.Workflow.Revision != applied.Workflow.Revision {
		t.Fatalf("replay was not a no-op: %#v", replay)
	}

	trimmed := map[string]any{
		"api_version": 1, "name": "SDLC", "initial_status": "technical_spec",
		"statuses": []map[string]any{
			status("technical_spec", "Technical Spec", "unstarted", "#8b5cf6"),
			status("implementation", "Implementation", "started", "#2563eb"),
		},
	}
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom", "spec": trimmed, "expected_revision": replay.Workflow.Revision,
		}), "id", projectID)).Want(http.StatusConflict)

	var archived issueWorkflowApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom", "spec": trimmed, "expected_revision": replay.Workflow.Revision, "allow_archive": true,
		}), "id", projectID)).Want(http.StatusOK).JSON(&archived)
	if len(archived.Plan.Archived) != 1 || archived.Plan.Archived[0] != "shipped" {
		t.Fatalf("archive apply = %#v", archived)
	}

	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "spec-created issue", "project_id": projectID,
		})).Want(http.StatusCreated).JSON(&created)
	if created.Status != "todo" || created.WorkflowStatusID == nil || *created.WorkflowStatusID != initialID {
		t.Fatalf("workflow-native create = %#v", created)
	}
	var explicitlyCreated IssueResponse
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "explicit workflow status issue", "project_id": projectID, "workflow_status_id": implementationID,
		})).Want(http.StatusCreated).JSON(&explicitlyCreated)
	if explicitlyCreated.Status != "in_progress" || explicitlyCreated.WorkflowStatusID == nil || *explicitlyCreated.WorkflowStatusID != implementationID {
		t.Fatalf("explicit workflow-native create = %#v", explicitlyCreated)
	}
	var transitioned transitionIssueStatusNodeResponse
	testutil.Call(t, testHandler.TransitionIssueStatusNode,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/transitions", map[string]any{
			"workflow_status_id": implementationID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&transitioned)
	if transitioned.Issue.Status != "in_progress" || transitioned.Issue.WorkflowStatusID == nil || *transitioned.Issue.WorkflowStatusID != implementationID {
		t.Fatalf("custom-node compatibility projection = %#v", transitioned)
	}
}

func TestCreateProjectWithWorkflowSpecCommitsOneMaterializedDefinition(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issueworkflow.EnsureDefault(ctx, testHandler.Queries.WithTx(tx), workspaceID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure workspace workflow: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	workflowPayload := map[string]any{
		"api_version": 1, "name": "GTM SEO", "initial_status": "brief",
		"statuses": []map[string]any{
			{"key": "brief", "name": "Brief", "color": "#8b5cf6", "phase": "unstarted"},
			{"key": "published", "name": "Published", "color": "#16a34a", "phase": "completed"},
		},
	}
	failureTitle := "Rolled back workflow project " + time.Now().Format("150405.000000000")
	testutil.Call(t, testHandler.CreateProject,
		newRequest(http.MethodPost, "/api/projects", map[string]any{
			"title": failureTitle, "issue_workflow": workflowPayload,
			"resources": []map[string]any{
				{"resource_type": "github_repo", "resource_ref": map[string]any{"url": "https://github.com/multica-ai/multica"}},
				{"resource_type": "github_repo", "resource_ref": map[string]any{"url": "https://github.com/multica-ai/multica"}},
			},
		})).Want(http.StatusConflict)
	var rolledBackProjects int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM project WHERE workspace_id = $1 AND title = $2`, workspaceID, failureTitle).Scan(&rolledBackProjects); err != nil {
		t.Fatal(err)
	}
	if rolledBackProjects != 0 {
		t.Fatalf("failed atomic create left %d project rows", rolledBackProjects)
	}

	var response struct {
		ProjectResponse
		IssueWorkflow *issueWorkflowResponse `json:"issue_workflow"`
	}
	testutil.Call(t, testHandler.CreateProject,
		newRequest(http.MethodPost, "/api/projects", map[string]any{
			"title":          "Atomic workflow project " + time.Now().Format("150405.000000000"),
			"issue_workflow": workflowPayload,
		})).Want(http.StatusCreated).JSON(&response)
	if response.ID == "" || response.IssueWorkflow == nil || response.IssueWorkflow.Mode != "custom" || len(response.IssueWorkflow.Statuses) != 2 {
		t.Fatalf("project create workflow response = %#v", response)
	}
	workflowID := response.IssueWorkflow.Workflow.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, response.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workflow_id = $1`, workflowID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, workflowID)
	})
	project, err := testHandler.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: parseUUID(response.ID), WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if !project.DefaultIssueWorkflowID.Valid || uuidToString(project.DefaultIssueWorkflowID) != workflowID {
		t.Fatalf("project workflow pointer = %v, want %s", project.DefaultIssueWorkflowID, workflowID)
	}
}
