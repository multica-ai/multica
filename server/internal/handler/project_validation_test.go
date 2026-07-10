package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestProjectOwnsExactlyOneSpace(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	key := fmt.Sprintf("P%06d", suffix%1_000_000)

	var spaceID, defaultSpaceID, projectID, issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Project Space %d", suffix), key, testUserID).Scan(&spaceID); err != nil {
		t.Fatalf("create Space fixture: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM workspace_space
		WHERE workspace_id = $1 AND id <> $2 AND archived_at IS NULL
		ORDER BY created_at, id LIMIT 1
	`, testWorkspaceID, spaceID).Scan(&defaultSpaceID); err != nil {
		t.Fatalf("load default Space fixture: %v", err)
	}
	t.Cleanup(func() {
		if issueID != "" {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if projectID != "" {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		}
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, spaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":    fmt.Sprintf("single Space project %d", suffix),
		"space_id": spaceID,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	projectID = project.ID
	if project.SpaceID != spaceID {
		t.Fatalf("Project space_id = %q, want %q", project.SpaceID, spaceID)
	}

	// A Project's Space is authoritative for new Project Issues when the caller
	// omits space_id.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      fmt.Sprintf("Project Space issue %d", suffix),
		"project_id": project.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue in Project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode CreateIssue: %v", err)
	}
	issueID = issue.ID
	if issue.SpaceID == nil || *issue.SpaceID != spaceID {
		t.Fatalf("Project Issue space_id = %v, want %q", issue.SpaceID, spaceID)
	}

	// Explicitly pairing the Project with a different Space is rejected.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      fmt.Sprintf("mismatched Project Space issue %d", suffix),
		"project_id": project.ID,
		"space_id":   defaultSpaceID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), projectSpaceMismatchMessage) {
		t.Fatalf("CreateIssue with mismatched Space: expected 400 mismatch, got %d: %s", w.Code, w.Body.String())
	}

	// Listing is a direct ownership filter, not a many-to-many membership join.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&space_id="+spaceID, nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects by Space: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listed struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode ListProjects: %v", err)
	}
	found := false
	for _, candidate := range listed.Projects {
		if candidate.ID == project.ID {
			found = true
			if candidate.SpaceID != spaceID {
				t.Fatalf("listed Project space_id = %q, want %q", candidate.SpaceID, spaceID)
			}
		}
	}
	if !found {
		t.Fatalf("Project %s missing from its owning Space filter", project.ID)
	}

	// Space changes require a future impact-aware Move Project workflow; the
	// generic metadata endpoint cannot silently move the whole issue tree.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID, map[string]any{"space_id": defaultSpaceID})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateProject space_id: expected 409, got %d: %s", w.Code, w.Body.String())
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
