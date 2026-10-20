package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// createProjectForTest inserts a project row directly: these tests exercise the
// numbering scope, not the create endpoint.
func createProjectForTest(t *testing.T, title, issuePrefix string) string {
	t.Helper()
	return dbfx.Insert(t, "project", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"title":        title,
		"issue_prefix": issuePrefix,
	})
}

func updateProjectPrefixForTest(t *testing.T, projectID, prefix string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+projectID, map[string]any{"issue_prefix": prefix})
	req = withURLParam(req, "id", projectID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	return updated
}

// TestProjectIssuePrefixScopesNumbering covers the core rule: a prefixed
// project numbers its own issues, while an issue without a prefixed project
// keeps the workspace counter, and both identifiers resolve back to their own
// issue.
func TestProjectIssuePrefixScopesNumbering(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	projectID := createProjectForTest(t, "Prefix scoped project "+t.Name(), "PROA")

	projectIssue := createIssueForTest(t, map[string]any{
		"title":      "project scoped issue " + t.Name(),
		"status":     "todo",
		"priority":   "medium",
		"project_id": projectID,
	})
	if projectIssue.Identifier != "PROA-1" {
		t.Fatalf("project issue identifier = %q, want PROA-1", projectIssue.Identifier)
	}
	if projectIssue.Number != 1 {
		t.Fatalf("project issue number = %d, want 1", projectIssue.Number)
	}

	workspaceIssue := createIssueForTest(t, map[string]any{
		"title":    "workspace scoped issue " + t.Name(),
		"status":   "todo",
		"priority": "medium",
	})
	// The workspace counter is shared with the rest of the suite, so assert the
	// shape rather than a fixed number: SHAR-<n>, and a different identifier
	// from the project-scoped issue even when both numbers collide.
	if want := "SHAR-" + strconv.Itoa(int(workspaceIssue.Number)); workspaceIssue.Identifier != want {
		t.Fatalf("workspace issue identifier = %q, want %q", workspaceIssue.Identifier, want)
	}
	if workspaceIssue.Identifier == projectIssue.Identifier {
		t.Fatalf("project and workspace identifiers must differ, both are %q", workspaceIssue.Identifier)
	}

	projectResolved, ok := testHandler.resolveIssueByIdentifier(context.Background(), "PROA-1", testWorkspaceID)
	if !ok || uuidToString(projectResolved.ID) != projectIssue.ID {
		t.Fatalf("PROA-1 resolved to %+v, want issue %s", projectResolved, projectIssue.ID)
	}
	workspaceResolved, ok := testHandler.resolveIssueByIdentifier(context.Background(), workspaceIssue.Identifier, testWorkspaceID)
	if !ok || uuidToString(workspaceResolved.ID) != workspaceIssue.ID {
		t.Fatalf("%s resolved to %+v, want issue %s", workspaceIssue.Identifier, workspaceResolved, workspaceIssue.ID)
	}
	if _, ok := testHandler.resolveIssueByIdentifier(context.Background(), "NOPE-1", testWorkspaceID); ok {
		t.Fatalf("unknown prefix must not resolve")
	}
}

// TestProjectIssuePrefixRenumbering checks that changing and clearing a
// project prefix renumbers that project's issues into the new scope.
func TestProjectIssuePrefixRenumbering(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	projectID := createProjectForTest(t, "Renumber project "+t.Name(), "PREN")

	first := createIssueForTest(t, map[string]any{
		"title": "renumber one " + t.Name(), "status": "todo", "priority": "medium", "project_id": projectID,
	})
	second := createIssueForTest(t, map[string]any{
		"title": "renumber two " + t.Name(), "status": "todo", "priority": "medium", "project_id": projectID,
	})
	if first.Identifier != "PREN-1" || second.Identifier != "PREN-2" {
		t.Fatalf("initial identifiers = %q, %q, want PREN-1, PREN-2", first.Identifier, second.Identifier)
	}

	updated := updateProjectPrefixForTest(t, projectID, "prbx")
	if updated.IssuePrefix != "PRBX" {
		t.Fatalf("project issue_prefix = %q, want normalized PRBX", updated.IssuePrefix)
	}
	renumbered, ok := testHandler.resolveIssueByIdentifier(context.Background(), "PRBX-1", testWorkspaceID)
	if !ok || uuidToString(renumbered.ID) != first.ID {
		t.Fatalf("PRBX-1 resolved to %+v, want issue %s", renumbered, first.ID)
	}
	renumberedSecond, ok := testHandler.resolveIssueByIdentifier(context.Background(), "PRBX-2", testWorkspaceID)
	if !ok || uuidToString(renumberedSecond.ID) != second.ID {
		t.Fatalf("PRBX-2 resolved to %+v, want issue %s", renumberedSecond, second.ID)
	}
	if _, ok := testHandler.resolveIssueByIdentifier(context.Background(), "PREN-1", testWorkspaceID); ok {
		t.Fatalf("the old prefix must stop resolving after a rename")
	}

	updateProjectPrefixForTest(t, projectID, "")
	cleared, ok := testHandler.resolveIssueByIdentifier(context.Background(), "SHAR-"+strconv.Itoa(int(workspaceNumberForTest(t, first.ID))), testWorkspaceID)
	if !ok || uuidToString(cleared.ID) != first.ID {
		t.Fatalf("cleared-prefix issue did not move to the workspace scope: %+v", cleared)
	}
}

func workspaceNumberForTest(t *testing.T, issueID string) int32 {
	t.Helper()
	var number int32
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, issueID).Scan(&number)
	return number
}

// TestProjectIssuePrefixConflicts covers the three ambiguity paths: a sibling
// project already owns the prefix, the prefix equals the workspace prefix, and
// the workspace prefix cannot be changed to one a project already owns.
func TestProjectIssuePrefixConflicts(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	createProjectForTest(t, "Owner project "+t.Name(), "POWN")

	createProject := func(prefix string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
			"title":        "conflict project " + t.Name(),
			"issue_prefix": prefix,
		})
		testHandler.CreateProject(w, req)
		return w
	}
	if w := createProject("POWN"); w.Code != http.StatusConflict {
		t.Fatalf("duplicate project prefix: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if w := createProject("shar"); w.Code != http.StatusConflict {
		t.Fatalf("workspace prefix reuse: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if w := createProject("bad prefix"); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid prefix: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := createProject("POK"); w.Code != http.StatusCreated {
		t.Fatalf("unused prefix: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The workspace prefix cannot move onto a prefix a project already owns.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID, map[string]any{"issue_prefix": "POWN"})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("workspace prefix takeover: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestProjectDeleteMovesIssuesToWorkspaceScope covers the fallback path: the FK
// becomes NULL on delete, so those issues must leave the project prefix.
func TestProjectDeleteMovesIssuesToWorkspaceScope(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	projectID := createProjectForTest(t, "Delete project "+t.Name(), "PDEL")

	issue := createIssueForTest(t, map[string]any{
		"title": "delete scoped issue " + t.Name(), "status": "todo", "priority": "medium", "project_id": projectID,
	})
	if issue.Identifier != "PDEL-1" {
		t.Fatalf("identifier before delete = %q, want PDEL-1", issue.Identifier)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("DELETE", "/api/projects/"+projectID, nil), "id", projectID)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if _, ok := testHandler.resolveIssueByIdentifier(context.Background(), "PDEL-1", testWorkspaceID); ok {
		t.Fatalf("the deleted project's prefix must stop resolving")
	}
	number := workspaceNumberForTest(t, issue.ID)
	moved, ok := testHandler.resolveIssueByIdentifier(context.Background(), "SHAR-"+strconv.Itoa(int(number)), testWorkspaceID)
	if !ok || uuidToString(moved.ID) != issue.ID {
		t.Fatalf("issue did not move to the workspace scope: %+v", moved)
	}
}

// TestIssueProjectMoveRenumbersScope covers moving one issue into a prefixed
// project through the ordinary update path.
func TestIssueProjectMoveRenumbersScope(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "SHAR")
	projectID := createProjectForTest(t, "Move target "+t.Name(), "PMOV")

	issue := createIssueForTest(t, map[string]any{
		"title": "movable issue " + t.Name(), "status": "todo", "priority": "medium",
	})
	if issue.Identifier == "PMOV-1" {
		t.Fatalf("issue created outside the project must not use its prefix")
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issue.ID, map[string]any{"project_id": projectID}), "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated issue: %v", err)
	}
	if updated.Identifier != "PMOV-1" {
		t.Fatalf("identifier after move = %q, want PMOV-1", updated.Identifier)
	}
	resolved, ok := testHandler.resolveIssueByIdentifier(context.Background(), "PMOV-1", testWorkspaceID)
	if !ok || uuidToString(resolved.ID) != issue.ID {
		t.Fatalf("PMOV-1 resolved to %+v, want issue %s", resolved, issue.ID)
	}
}
