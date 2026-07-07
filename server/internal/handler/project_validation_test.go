package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An unknown project status must fail fast with a 400 and the valid list, not
// surface the DB CHECK violation as a 500 (#3925: `--status active`).
func TestCreateProjectInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "invalid status project",
		"status": "active",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "planned") {
		t.Errorf("expected error to list valid statuses, got: %s", body)
	}
}

func TestCreateProjectInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "invalid priority project",
		"priority": "critical",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}

// A valid status still creates the project (the validation does not over-reject).
func TestCreateProjectValidStatusReturns201(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "valid status project",
		"status": "in_progress",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid status, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})
	if project.Status != "in_progress" {
		t.Errorf("expected status in_progress, got %q", project.Status)
	}
}

func TestProjectIssuePrefixOverridesIssueIdentifierDisplay(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "CAL")

	projectW := httptest.NewRecorder()
	projectReq := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":        "project prefix project",
		"issue_prefix": "LOL",
	})
	testHandler.CreateProject(projectW, projectReq)
	if projectW.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", projectW.Code, projectW.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(projectW.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})
	if project.IssuePrefix == nil || *project.IssuePrefix != "LOL" {
		t.Fatalf("CreateProject issue_prefix = %v, want LOL", project.IssuePrefix)
	}

	issueW := httptest.NewRecorder()
	issueReq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "issue with project prefix",
		"project_id": project.ID,
	})
	testHandler.CreateIssue(issueW, issueReq)
	if issueW.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", issueW.Code, issueW.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(issueW.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateIssue: %v", err)
	}
	wantProjectIdentifier := fmt.Sprintf("LOL-%d", created.Number)
	if created.Identifier != wantProjectIdentifier {
		t.Fatalf("CreateIssue identifier = %q, want %q", created.Identifier, wantProjectIdentifier)
	}

	listW := httptest.NewRecorder()
	listReq := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&project_id="+project.ID, nil)
	testHandler.ListIssues(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listed struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listed); err != nil {
		t.Fatalf("decode ListIssues: %v", err)
	}
	if len(listed.Issues) != 1 {
		t.Fatalf("ListIssues returned %d issues, want 1", len(listed.Issues))
	}
	if listed.Issues[0].Identifier != wantProjectIdentifier {
		t.Fatalf("ListIssues identifier = %q, want %q", listed.Issues[0].Identifier, wantProjectIdentifier)
	}

	getW := httptest.NewRecorder()
	getReq := newRequest("GET", "/api/issues/"+created.ID, nil)
	getReq = withURLParam(getReq, "id", created.ID)
	testHandler.GetIssue(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetIssue: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var fetched IssueResponse
	if err := json.NewDecoder(getW.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode GetIssue: %v", err)
	}
	if fetched.Identifier != wantProjectIdentifier {
		t.Fatalf("GetIssue identifier = %q, want %q", fetched.Identifier, wantProjectIdentifier)
	}

	byIdentifierW := httptest.NewRecorder()
	byIdentifierReq := newRequest("GET", "/api/issues/"+wantProjectIdentifier, nil)
	byIdentifierReq = withURLParam(byIdentifierReq, "id", wantProjectIdentifier)
	testHandler.GetIssue(byIdentifierW, byIdentifierReq)
	if byIdentifierW.Code != http.StatusOK {
		t.Fatalf("GetIssue by project identifier: expected 200, got %d: %s", byIdentifierW.Code, byIdentifierW.Body.String())
	}

	clearW := httptest.NewRecorder()
	clearReq := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"issue_prefix": "",
	})
	clearReq = withURLParam(clearReq, "id", project.ID)
	testHandler.UpdateProject(clearW, clearReq)
	if clearW.Code != http.StatusOK {
		t.Fatalf("UpdateProject clear issue_prefix: expected 200, got %d: %s", clearW.Code, clearW.Body.String())
	}
	var cleared ProjectResponse
	if err := json.NewDecoder(clearW.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode UpdateProject clear: %v", err)
	}
	if cleared.IssuePrefix != nil {
		t.Fatalf("cleared project issue_prefix = %v, want nil", *cleared.IssuePrefix)
	}

	fallbackW := httptest.NewRecorder()
	fallbackReq := newRequest("GET", "/api/issues/"+created.ID, nil)
	fallbackReq = withURLParam(fallbackReq, "id", created.ID)
	testHandler.GetIssue(fallbackW, fallbackReq)
	if fallbackW.Code != http.StatusOK {
		t.Fatalf("GetIssue after clear: expected 200, got %d: %s", fallbackW.Code, fallbackW.Body.String())
	}
	var fallback IssueResponse
	if err := json.NewDecoder(fallbackW.Body).Decode(&fallback); err != nil {
		t.Fatalf("decode GetIssue after clear: %v", err)
	}
	wantWorkspaceIdentifier := fmt.Sprintf("CAL-%d", created.Number)
	if fallback.Identifier != wantWorkspaceIdentifier {
		t.Fatalf("identifier after clear = %q, want %q", fallback.Identifier, wantWorkspaceIdentifier)
	}
}

// Updating to an unknown status is a 400, not a 500.
func TestUpdateProjectInvalidStatusReturns400(t *testing.T) {
	// Seed a project to update.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "update validation project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID, map[string]any{"status": "active"})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid update status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteProjectRequiresAdminOrOwner(t *testing.T) {
	memberUserID := createProjectPermissionTestMember(t, "member")
	project := createProjectPermissionTestProject(t, "delete permission denied project")

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, project.ID).Scan(&exists); err != nil {
		t.Fatalf("verify project exists: %v", err)
	}
	if !exists {
		t.Fatal("project was deleted despite plain member request")
	}
}

func TestDeleteProjectAllowsAdmin(t *testing.T) {
	adminUserID := createProjectPermissionTestMember(t, "admin")
	project := createProjectPermissionTestProject(t, "delete permission admin project")

	w := httptest.NewRecorder()
	req := newRequestAs(adminUserID, "DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin project delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, project.ID).Scan(&exists); err != nil {
		t.Fatalf("verify project deleted: %v", err)
	}
	if exists {
		t.Fatal("project still exists after admin delete")
	}
}

func createProjectPermissionTestMember(t *testing.T, role string) string {
	t.Helper()

	ctx := context.Background()
	email := "project-delete-" + role + "@multica.test"
	// The schema uses no foreign keys or cascades, so a leftover member from a
	// prior run won't disappear when its user is deleted. Drop the member first.
	_, _ = testPool.Exec(ctx, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email = $1)`, email)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	var userID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email)
VALUES ($1, $2)
RETURNING id
`, "Project Delete "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create %s user: %v", role, err)
	}
	t.Cleanup(func() {
		// No cascade in the schema: remove the member row before its user so the
		// shared test workspace isn't left with an orphaned member record.
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create %s member: %v", role, err)
	}

	return userID
}

func createProjectPermissionTestProject(t *testing.T, title string) ProjectResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})
	return project
}
