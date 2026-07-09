package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createReconcileTestSpace creates a Space via the handler and registers
// cleanup. Tests in this file need multiple Spaces in the same workspace to
// exercise the project→space removal reconciliation.
func createReconcileTestSpace(t *testing.T, name, key string) SpaceResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/spaces", map[string]any{"name": name, "key": key})
	testHandler.CreateSpace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSpace(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var space SpaceResponse
	if err := json.NewDecoder(w.Body).Decode(&space); err != nil {
		t.Fatalf("decode CreateSpace(%s): %v", name, err)
	}
	// No delete endpoint for Spaces (archive-only, see space.go) — cleanup
	// hard-deletes the row directly. Issue/autopilot FKs into workspace_space
	// have no ON DELETE CASCADE (131_workspace_space.up.sql), so every issue
	// created against this Space in a test must be cleaned up (registered
	// via createReconcileTestIssue) before this runs; t.Cleanup's LIFO order
	// takes care of that as long as issues are created after the Space.
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, space.ID)
	})
	return space
}

func createReconcileTestProject(t *testing.T, title string, spaceIDs []string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":     title,
		"space_ids": spaceIDs,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject(%s): expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject(%s): %v", title, err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})
	return project
}

func createReconcileTestIssue(t *testing.T, title, spaceID, projectID string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      title,
		"space_id":   spaceID,
		"project_id": projectID,
		"status":     "todo",
		"priority":   "none",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue(%s): expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode CreateIssue(%s): %v", title, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_identifier_alias WHERE issue_id = $1`, issue.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

// Removing a Space from a project's space set is fine when nothing under
// this project sits in that Space — the "unguarded by design" creation-time
// default still holds for the empty case.
func TestUpdateProjectRemoveEmptySpaceSucceeds(t *testing.T) {
	spaceA := createReconcileTestSpace(t, "Reconcile A", "RCA")
	spaceB := createReconcileTestSpace(t, "Reconcile B", "RCB")
	project := createReconcileTestProject(t, "remove empty space project", []string{spaceA.ID, spaceB.ID})

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"space_ids": []string{spaceA.ID},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProject: %v", err)
	}
	if len(updated.SpaceIDs) != 1 || updated.SpaceIDs[0] != spaceA.ID {
		t.Fatalf("expected space_ids [%s], got %v", spaceA.ID, updated.SpaceIDs)
	}
}

// Removing a Space that still has issues under this project, with no
// space_reassignments, must not silently strand those issues: reject with a
// structured 409 that names the Space and how many issues are at stake.
func TestUpdateProjectRemoveSpaceWithIssuesRequiresReassignment(t *testing.T) {
	spaceA := createReconcileTestSpace(t, "Reconcile C", "RCC")
	spaceB := createReconcileTestSpace(t, "Reconcile D", "RCD")
	project := createReconcileTestProject(t, "remove space with issues project", []string{spaceA.ID, spaceB.ID})
	issue := createReconcileTestIssue(t, "stranded issue", spaceA.ID, project.ID)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"space_ids": []string{spaceB.ID},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflict projectSpaceConflictResponse
	if err := json.NewDecoder(w.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if conflict.Code != "project_space_has_issues" {
		t.Fatalf("expected code project_space_has_issues, got %q", conflict.Code)
	}
	if len(conflict.SpacesWithIssues) != 1 {
		t.Fatalf("expected exactly 1 conflicting space, got %d: %+v", len(conflict.SpacesWithIssues), conflict.SpacesWithIssues)
	}
	got := conflict.SpacesWithIssues[0]
	if got.SpaceID != spaceA.ID || got.SpaceKey != spaceA.Key || got.IssueCount != 1 {
		t.Fatalf("unexpected conflict entry: %+v", got)
	}

	// The project's space set and the issue's space are both untouched.
	verify := httptest.NewRecorder()
	getReq := newRequest("GET", "/api/issues/"+issue.ID, nil)
	getReq = withURLParam(getReq, "id", issue.ID)
	testHandler.GetIssue(verify, getReq)
	var reloaded IssueResponse
	if err := json.NewDecoder(verify.Body).Decode(&reloaded); err != nil {
		t.Fatalf("decode GetIssue: %v", err)
	}
	if reloaded.SpaceID == nil || *reloaded.SpaceID != spaceA.ID {
		t.Fatalf("expected issue to stay in space %s, got %v", spaceA.ID, reloaded.SpaceID)
	}
}

// Supplying a valid space_reassignments target moves the stranded issues
// into that Space (renumbering them and recording the old identifier as an
// alias) in the same request that removes the Space from the project.
func TestUpdateProjectRemoveSpaceWithReassignmentMovesIssues(t *testing.T) {
	spaceA := createReconcileTestSpace(t, "Reconcile E", "RCE")
	spaceB := createReconcileTestSpace(t, "Reconcile F", "RCF")
	project := createReconcileTestProject(t, "reassign project", []string{spaceA.ID, spaceB.ID})
	issue := createReconcileTestIssue(t, "reassigned issue", spaceA.ID, project.ID)
	oldIdentifier := issue.Identifier

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"space_ids":           []string{spaceB.ID},
		"space_reassignments": map[string]string{spaceA.ID: spaceB.ID},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProject: %v", err)
	}
	if len(updated.SpaceIDs) != 1 || updated.SpaceIDs[0] != spaceB.ID {
		t.Fatalf("expected space_ids [%s], got %v", spaceB.ID, updated.SpaceIDs)
	}

	verify := httptest.NewRecorder()
	getReq := newRequest("GET", "/api/issues/"+issue.ID, nil)
	getReq = withURLParam(getReq, "id", issue.ID)
	testHandler.GetIssue(verify, getReq)
	var reloaded IssueResponse
	if err := json.NewDecoder(verify.Body).Decode(&reloaded); err != nil {
		t.Fatalf("decode GetIssue: %v", err)
	}
	if reloaded.SpaceID == nil || *reloaded.SpaceID != spaceB.ID {
		t.Fatalf("expected issue moved to space %s, got %v", spaceB.ID, reloaded.SpaceID)
	}
	if reloaded.Identifier == oldIdentifier {
		t.Fatalf("expected a new identifier after the move, still %s", oldIdentifier)
	}

	// The old identifier keeps resolving via the alias table.
	resolveW := httptest.NewRecorder()
	resolveReq := newRequest("GET", "/api/issues/"+oldIdentifier, nil)
	resolveReq = withURLParam(resolveReq, "id", oldIdentifier)
	testHandler.GetIssue(resolveW, resolveReq)
	if resolveW.Code != http.StatusOK {
		t.Fatalf("expected old identifier %s to still resolve, got %d: %s", oldIdentifier, resolveW.Code, resolveW.Body.String())
	}
	var resolved IssueResponse
	if err := json.NewDecoder(resolveW.Body).Decode(&resolved); err != nil {
		t.Fatalf("decode GetIssue by old identifier: %v", err)
	}
	if resolved.ID != issue.ID {
		t.Fatalf("expected old identifier to resolve to issue %s, got %s", issue.ID, resolved.ID)
	}
}

// A reassignment target that isn't itself part of the project's new space
// set is invalid — you cannot move stranded issues into a Space the project
// won't list.
func TestUpdateProjectReassignmentTargetMustRemainOnProject(t *testing.T) {
	spaceA := createReconcileTestSpace(t, "Reconcile G", "RCG")
	spaceB := createReconcileTestSpace(t, "Reconcile H", "RCH")
	spaceC := createReconcileTestSpace(t, "Reconcile I", "RCI")
	project := createReconcileTestProject(t, "invalid reassignment target project", []string{spaceA.ID, spaceB.ID})
	createReconcileTestIssue(t, "issue for invalid target", spaceA.ID, project.ID)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"space_ids":           []string{spaceB.ID},
		"space_reassignments": map[string]string{spaceA.ID: spaceC.ID},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a target outside the project's new space set, got %d: %s", w.Code, w.Body.String())
	}
}
