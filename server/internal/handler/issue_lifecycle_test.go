package handler

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectLifecycleAPIAndStatusNodeTransition(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lifecycle bootstrap: %v", err)
	}
	workspaceLifecycle, err := issuelifecycle.EnsureDefault(ctx, testHandler.Queries.WithTx(tx), workspaceID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure workspace lifecycle: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lifecycle bootstrap: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, workspaceID, "Lifecycle API "+time.Now().Format("150405.000000000")).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	var destinationProjectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, workspaceID, "Lifecycle move target "+time.Now().Format("150405.000000000")).Scan(&destinationProjectID); err != nil {
		t.Fatalf("create destination project: %v", err)
	}
	t.Cleanup(func() {
		var customLifecycleID pgtype.UUID
		_ = testPool.QueryRow(context.Background(), `
			SELECT id FROM issue_lifecycle
			WHERE workspace_id = $1 AND scope_type = 'project' AND scope_id = $2
		`, workspaceID, projectID).Scan(&customLifecycleID)
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM automation_execution WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)
		`, projectID)
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM issue_transition WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)
		`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id IN ($1, $2)`, projectID, destinationProjectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id IN ($1, $2)`, projectID, destinationProjectID)
		if customLifecycleID.Valid {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle_status WHERE lifecycle_id = $1`, customLifecycleID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle WHERE id = $1`, customLifecycleID)
		}
	})

	var customized issueLifecycleResponse
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "custom",
		}), "id", projectID)).Want(http.StatusOK).JSON(&customized)
	if customized.Mode != "custom" || customized.Lifecycle.ScopeType != "project" || customized.Lifecycle.ScopeID != projectID {
		t.Fatalf("custom lifecycle response = %#v", customized)
	}
	if len(customized.Statuses) < 7 {
		t.Fatalf("custom lifecycle status count = %d, want at least 7", len(customized.Statuses))
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
		t.Fatalf("custom lifecycle is missing required status nodes: %#v", customized.Statuses)
	}

	// Definition mutations advance the lifecycle version once. Entry Policy
	// has its own revision so a worker can pin the exact instructions it ran.
	definitionRevision := customized.Lifecycle.Revision
	var updated issueLifecycleResponse
	updateBody := func(revision int64) map[string]any {
		return map[string]any{
			"expected_revision": revision,
			"name":              "Building",
			"entry_policy": map[string]any{
				"assignee":     map[string]any{"type": "human", "id": testUserID},
				"executor":     map[string]any{"type": "none"},
				"instructions": "Wait for a human confirmation.",
				"advance":      "human_confirms",
			},
		}
	}
	testutil.Call(t, testHandler.UpdateIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+inProgressID, updateBody(definitionRevision)),
			"lifecycleId", customized.Lifecycle.ID, "statusId", inProgressID)).Want(http.StatusOK).JSON(&updated)
	if updated.Lifecycle.Revision != definitionRevision+1 {
		t.Fatalf("updated lifecycle revision = %d, want %d", updated.Lifecycle.Revision, definitionRevision+1)
	}
	var updatedNode issueLifecycleStatusResponse
	for _, status := range updated.Statuses {
		if status.ID == inProgressID {
			updatedNode = status
			break
		}
	}
	if updatedNode.Name != "Building" || updatedNode.EntryPolicyRevision != 2 ||
		updatedNode.EntryPolicy.Assignee.Type != issuelifecycle.AssigneeHuman ||
		updatedNode.EntryPolicy.Assignee.ID != testUserID {
		t.Fatalf("updated lifecycle node = %#v", updatedNode)
	}

	// Repeating the same semantic policy is a stable no-op for both versions.
	var noopDefinition issueLifecycleResponse
	testutil.Call(t, testHandler.UpdateIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+inProgressID, updateBody(updated.Lifecycle.Revision)),
			"lifecycleId", customized.Lifecycle.ID, "statusId", inProgressID)).Want(http.StatusOK).JSON(&noopDefinition)
	if noopDefinition.Lifecycle.Revision != updated.Lifecycle.Revision {
		t.Fatalf("no-op lifecycle revision = %d, want %d", noopDefinition.Lifecycle.Revision, updated.Lifecycle.Revision)
	}
	testutil.Call(t, testHandler.UpdateIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+inProgressID, updateBody(definitionRevision)),
			"lifecycleId", customized.Lifecycle.ID, "statusId", inProgressID)).Want(http.StatusConflict)

	testutil.Call(t, testHandler.UpdateIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+inProgressID, map[string]any{
			"expected_revision": updated.Lifecycle.Revision,
			"entry_policy": map[string]any{
				"assignee": map[string]any{"type": "keep"}, "executor": map[string]any{"type": "none"},
				"instructions": "", "advance": "executor_may_transition",
			},
		}), "lifecycleId", customized.Lifecycle.ID, "statusId", inProgressID)).Want(http.StatusBadRequest)

	// Reordering replaces the complete active order atomically.
	statusIDs := make([]string, len(updated.Statuses))
	for i := range updated.Statuses {
		statusIDs[len(updated.Statuses)-1-i] = updated.Statuses[i].ID
	}
	var reordered issueLifecycleResponse
	testutil.Call(t, testHandler.ReorderIssueLifecycleStatuses,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/reorder", map[string]any{
			"expected_revision": updated.Lifecycle.Revision, "status_ids": statusIDs,
		}), "lifecycleId", customized.Lifecycle.ID)).Want(http.StatusOK).JSON(&reordered)
	if reordered.Lifecycle.Revision != updated.Lifecycle.Revision+1 || reordered.Statuses[0].ID != statusIDs[0] {
		t.Fatalf("reordered lifecycle = %#v", reordered)
	}

	// Archive retains the node in definition history but prevents future use.
	if reordered.Lifecycle.InitialStatusID == nil {
		t.Fatal("custom lifecycle is missing its initial status")
	}
	testutil.Call(t, testHandler.ArchiveIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+*reordered.Lifecycle.InitialStatusID+"?expected_revision="+strconv.FormatInt(reordered.Lifecycle.Revision, 10), nil),
			"lifecycleId", customized.Lifecycle.ID, "statusId", *reordered.Lifecycle.InitialStatusID)).Want(http.StatusConflict)

	var archived issueLifecycleResponse
	testutil.Call(t, testHandler.ArchiveIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+backlogID+"?expected_revision="+strconv.FormatInt(reordered.Lifecycle.Revision, 10), nil),
			"lifecycleId", customized.Lifecycle.ID, "statusId", backlogID)).Want(http.StatusOK).JSON(&archived)
	if archived.Lifecycle.Revision != reordered.Lifecycle.Revision+1 {
		t.Fatalf("archived lifecycle revision = %d, want %d", archived.Lifecycle.Revision, reordered.Lifecycle.Revision+1)
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
	testutil.Call(t, testHandler.UpdateIssueLifecycleStatus,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issue-lifecycles/"+customized.Lifecycle.ID+"/statuses/"+backlogID, map[string]any{"expected_revision": archived.Lifecycle.Revision, "name": "Later"}),
			"lifecycleId", customized.Lifecycle.ID, "statusId", backlogID)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "archived lifecycle status", "status": "backlog", "project_id": projectID,
		})).Want(http.StatusConflict)
	customized = archived

	var effective issueLifecycleResponse
	testutil.Call(t, testHandler.GetEffectiveIssueLifecycle,
		newRequest(http.MethodGet, "/api/issue-lifecycles/effective?project_id="+projectID, nil)).Want(http.StatusOK).JSON(&effective)
	if effective.Lifecycle.ID != customized.Lifecycle.ID || effective.Mode != "custom" {
		t.Fatalf("effective lifecycle response = %#v", effective)
	}
	var concrete issueLifecycleResponse
	testutil.Call(t, testHandler.GetIssueLifecycle,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/issue-lifecycles/"+customized.Lifecycle.ID, nil),
			"lifecycleId", customized.Lifecycle.ID)).Want(http.StatusOK).JSON(&concrete)
	if concrete.Lifecycle.ID != customized.Lifecycle.ID || len(concrete.Statuses) != len(customized.Statuses) {
		t.Fatalf("concrete lifecycle response = %#v", concrete)
	}

	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "lifecycle API issue", "status": "todo", "project_id": projectID,
		})).Want(http.StatusCreated).JSON(&created)
	if created.LifecycleID == nil || *created.LifecycleID != customized.Lifecycle.ID || created.LifecycleStatusID == nil {
		t.Fatalf("created issue lifecycle binding = %#v", created)
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
	if listedCreated == nil || listedCreated.LifecycleID == nil || *listedCreated.LifecycleID != customized.Lifecycle.ID || listedCreated.LifecycleStatusID == nil || listedCreated.TransitionID == nil {
		t.Fatalf("list response omitted canonical lifecycle cursors: %#v", listedCreated)
	}

	var transitioned transitionIssueStatusNodeResponse
	testutil.Call(t, testHandler.TransitionIssueStatusNode,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/transitions", map[string]any{
			"lifecycle_status_id":    inProgressID,
			"expected_revision":      created.Revision,
			"expected_transition_id": created.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&transitioned)
	if transitioned.Issue.Status != "in_progress" || transitioned.Issue.LifecycleStatusID == nil || *transitioned.Issue.LifecycleStatusID != inProgressID {
		t.Fatalf("status-node transition response = %#v", transitioned)
	}
	if transitioned.Transition == nil || transitioned.Transition.ID == "" || transitioned.Transition.ToStatusID != inProgressID {
		t.Fatalf("transition audit response = %#v", transitioned.Transition)
	}
	if transitioned.Execution == nil || transitioned.Execution.Status != "dormant" || transitioned.Execution.TriggerTransitionID != transitioned.Transition.ID || transitioned.TaskID != nil {
		t.Fatalf("manual entry execution response = %#v", transitioned)
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
			"lifecycle_status_id":    inProgressID,
			"expected_revision":      transitioned.Issue.Revision,
			"expected_transition_id": transitioned.Issue.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&noop)
	if noop.Transition != nil || noop.Execution != nil || noop.TaskID != nil || noop.Issue.Revision != transitioned.Issue.Revision {
		t.Fatalf("same-node transition should be a stable no-op: %#v", noop)
	}

	var inherited issueLifecycleResponse
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "default",
		}), "id", projectID)).Want(http.StatusOK).JSON(&inherited)
	if inherited.Mode != "default" || inherited.Lifecycle.ID != uuidToString(workspaceLifecycle.ID) {
		t.Fatalf("default lifecycle response = %#v", inherited)
	}
	pinned, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("reload pinned issue: %v", err)
	}
	if uuidToString(pinned.LifecycleID) != customized.Lifecycle.ID {
		t.Fatalf("switching project default drifted existing issue lifecycle to %s", uuidToString(pinned.LifecycleID))
	}

	// A project move that crosses lifecycle definitions cannot guess a mapping
	// from matching names or legacy keys. The client must choose a target node.
	testutil.Call(t, testHandler.UpdateIssue,
		withURLParam(newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
			"project_id": destinationProjectID,
		}), "id", created.ID)).Want(http.StatusConflict)
	workspaceInProgress, err := testHandler.Queries.GetIssueLifecycleStatusByLegacyKey(ctx, db.GetIssueLifecycleStatusByLegacyKeyParams{
		WorkspaceID: workspaceID, LifecycleID: workspaceLifecycle.ID,
		LegacyStatusKey: pgtype.Text{String: "in_progress", Valid: true},
	})
	if err != nil {
		t.Fatalf("load workspace in-progress node: %v", err)
	}
	var moved IssueResponse
	testutil.Call(t, testHandler.UpdateIssue,
		withURLParam(newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
			"project_id":             destinationProjectID,
			"lifecycle_status_id":    uuidToString(workspaceInProgress.ID),
			"expected_revision":      transitioned.Issue.Revision,
			"expected_transition_id": transitioned.Issue.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&moved)
	if moved.ProjectID == nil || *moved.ProjectID != destinationProjectID || moved.LifecycleID == nil || *moved.LifecycleID != uuidToString(workspaceLifecycle.ID) || moved.LifecycleStatusID == nil || *moved.LifecycleStatusID != uuidToString(workspaceInProgress.ID) {
		t.Fatalf("explicit cross-lifecycle move = %#v", moved)
	}

	reloaded, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("reload existing issue: %v", err)
	}
	if uuidToString(reloaded.LifecycleID) != uuidToString(workspaceLifecycle.ID) {
		t.Fatalf("cross-lifecycle move did not bind target lifecycle: %s", uuidToString(reloaded.LifecycleID))
	}

	// The storage row is still a stable status-node binding, not merely the
	// compatibility key asserted above.
	status, err := testHandler.Queries.GetIssueLifecycleStatusByID(ctx, db.GetIssueLifecycleStatusByIDParams{
		WorkspaceID: workspaceID, LifecycleID: reloaded.LifecycleID, ID: reloaded.LifecycleStatusID,
	})
	if err != nil || uuidToString(status.ID) != uuidToString(workspaceInProgress.ID) {
		t.Fatalf("load canonical status node = %#v, err=%v", status, err)
	}
}

func TestProjectLifecycleSpecApplyIsDeclarativeAndLifecycleNative(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workspaceLifecycle, err := issuelifecycle.EnsureDefault(ctx, testHandler.Queries.WithTx(tx), workspaceID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure workspace lifecycle: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, workspaceID, "Lifecycle spec "+time.Now().Format("150405.000000000")).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	var lifecycleID string
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM automation_execution WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_transition WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		if lifecycleID != "" {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle_status WHERE lifecycle_id = $1`, lifecycleID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle WHERE id = $1`, lifecycleID)
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
	var preview issueLifecycleApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "custom", "spec": spec, "dry_run": true, "expected_revision": workspaceLifecycle.Revision,
		}), "id", projectID)).Want(http.StatusOK).JSON(&preview)
	if !preview.DryRun || !preview.Plan.Changed || len(preview.Plan.Created) != 3 {
		t.Fatalf("dry-run preview = %#v", preview)
	}
	var customCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_lifecycle WHERE workspace_id = $1 AND scope_type = 'project' AND scope_id = $2`, workspaceID, projectID).Scan(&customCount); err != nil {
		t.Fatal(err)
	}
	if customCount != 0 {
		t.Fatalf("dry-run persisted %d project lifecycle rows", customCount)
	}
	var applied issueLifecycleApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "custom", "spec": spec,
		}), "id", projectID)).Want(http.StatusOK).JSON(&applied)
	lifecycleID = applied.Lifecycle.ID
	if !applied.Plan.Changed || len(applied.Plan.Created) != 3 || applied.Lifecycle.InitialStatusID == nil {
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
	if initialID == "" || implementationID == "" || *applied.Lifecycle.InitialStatusID != initialID {
		t.Fatalf("initial status binding = %#v", applied)
	}

	var replay issueLifecycleApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "custom", "spec": spec, "expected_revision": applied.Lifecycle.Revision,
		}), "id", projectID)).Want(http.StatusOK).JSON(&replay)
	if replay.Plan.Changed || replay.Lifecycle.Revision != applied.Lifecycle.Revision {
		t.Fatalf("replay was not a no-op: %#v", replay)
	}

	trimmed := map[string]any{
		"api_version": 1, "name": "SDLC", "initial_status": "technical_spec",
		"statuses": []map[string]any{
			status("technical_spec", "Technical Spec", "unstarted", "#8b5cf6"),
			status("implementation", "Implementation", "started", "#2563eb"),
		},
	}
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "custom", "spec": trimmed, "expected_revision": replay.Lifecycle.Revision,
		}), "id", projectID)).Want(http.StatusConflict)

	var archived issueLifecycleApplyResponse
	testutil.Call(t, testHandler.UpdateProjectIssueLifecycle,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-lifecycle", map[string]any{
			"mode": "custom", "spec": trimmed, "expected_revision": replay.Lifecycle.Revision, "allow_archive": true,
		}), "id", projectID)).Want(http.StatusOK).JSON(&archived)
	if len(archived.Plan.Archived) != 1 || archived.Plan.Archived[0] != "shipped" {
		t.Fatalf("archive apply = %#v", archived)
	}

	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "spec-created issue", "project_id": projectID,
		})).Want(http.StatusCreated).JSON(&created)
	if created.Status != "todo" || created.LifecycleStatusID == nil || *created.LifecycleStatusID != initialID {
		t.Fatalf("lifecycle-native create = %#v", created)
	}
	var explicitlyCreated IssueResponse
	testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "explicit lifecycle status issue", "project_id": projectID, "lifecycle_status_id": implementationID,
		})).Want(http.StatusCreated).JSON(&explicitlyCreated)
	if explicitlyCreated.Status != "in_progress" || explicitlyCreated.LifecycleStatusID == nil || *explicitlyCreated.LifecycleStatusID != implementationID {
		t.Fatalf("explicit lifecycle-native create = %#v", explicitlyCreated)
	}
	var transitioned transitionIssueStatusNodeResponse
	testutil.Call(t, testHandler.TransitionIssueStatusNode,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/transitions", map[string]any{
			"lifecycle_status_id": implementationID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&transitioned)
	if transitioned.Issue.Status != "in_progress" || transitioned.Issue.LifecycleStatusID == nil || *transitioned.Issue.LifecycleStatusID != implementationID {
		t.Fatalf("custom-node compatibility projection = %#v", transitioned)
	}
}

func TestCreateProjectWithLifecycleSpecCommitsOneMaterializedDefinition(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	seedTestCatalog(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuelifecycle.EnsureDefault(ctx, testHandler.Queries.WithTx(tx), workspaceID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure workspace lifecycle: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	lifecyclePayload := map[string]any{
		"api_version": 1, "name": "GTM SEO", "initial_status": "brief",
		"statuses": []map[string]any{
			{"key": "brief", "name": "Brief", "color": "#8b5cf6", "phase": "unstarted"},
			{"key": "published", "name": "Published", "color": "#16a34a", "phase": "completed"},
		},
	}
	failureTitle := "Rolled back lifecycle project " + time.Now().Format("150405.000000000")
	testutil.Call(t, testHandler.CreateProject,
		newRequest(http.MethodPost, "/api/projects", map[string]any{
			"title": failureTitle, "lifecycle": lifecyclePayload,
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
		IssueLifecycle *issueLifecycleResponse `json:"issue_lifecycle"`
	}
	testutil.Call(t, testHandler.CreateProject,
		newRequest(http.MethodPost, "/api/projects", map[string]any{
			"title":     "Atomic lifecycle project " + time.Now().Format("150405.000000000"),
			"lifecycle": lifecyclePayload,
		})).Want(http.StatusCreated).JSON(&response)
	if response.ID == "" || response.IssueLifecycle == nil || response.IssueLifecycle.Mode != "custom" || len(response.IssueLifecycle.Statuses) != 2 {
		t.Fatalf("project create lifecycle response = %#v", response)
	}
	lifecycleID := response.IssueLifecycle.Lifecycle.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, response.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle_status WHERE lifecycle_id = $1`, lifecycleID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_lifecycle WHERE id = $1`, lifecycleID)
	})
	project, err := testHandler.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: parseUUID(response.ID), WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if !project.DefaultIssueLifecycleID.Valid || uuidToString(project.DefaultIssueLifecycleID) != lifecycleID {
		t.Fatalf("project lifecycle pointer = %v, want %s", project.DefaultIssueLifecycleID, lifecycleID)
	}
}
