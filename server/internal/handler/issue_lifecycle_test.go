package handler

import (
	"context"
	"net/http"
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

	var effective issueLifecycleResponse
	testutil.Call(t, testHandler.GetEffectiveIssueLifecycle,
		newRequest(http.MethodGet, "/api/issue-lifecycles/effective?project_id="+projectID, nil)).Want(http.StatusOK).JSON(&effective)
	if effective.Lifecycle.ID != customized.Lifecycle.ID || effective.Mode != "custom" {
		t.Fatalf("effective lifecycle response = %#v", effective)
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

	var inProgressID string
	for _, status := range customized.Statuses {
		if status.LegacyStatusKey != nil && *status.LegacyStatusKey == "in_progress" {
			inProgressID = status.ID
			break
		}
	}
	if inProgressID == "" {
		t.Fatal("custom lifecycle has no in_progress status")
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
	var noop transitionIssueStatusNodeResponse
	testutil.Call(t, testHandler.TransitionIssueStatusNode,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/transitions", map[string]any{
			"lifecycle_status_id":    inProgressID,
			"expected_revision":      transitioned.Issue.Revision,
			"expected_transition_id": transitioned.Issue.TransitionID,
		}), "id", created.ID)).Want(http.StatusOK).JSON(&noop)
	if noop.Transition != nil || noop.Issue.Revision != transitioned.Issue.Revision {
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
