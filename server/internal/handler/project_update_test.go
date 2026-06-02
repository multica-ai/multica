package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// createProjectUpdateTestProject seeds a project in the default test workspace
// and registers cleanup. It returns the decoded project response.
func createProjectUpdateTestProject(t *testing.T, title string) ProjectResponse {
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
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	return project
}

// TestProjectUpdateCreateListAndDerivedHealth covers the happy path: posting a
// health update, listing it back with the body intact, and confirming the
// project's derived "health" field reflects the latest update.
func TestProjectUpdateCreateListAndDerivedHealth(t *testing.T) {
	project := createProjectUpdateTestProject(t, "Project update happy path")

	const body = "Sprint slipped; mitigations in flight."

	// Create an at_risk update.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/updates", map[string]any{
		"health": "at_risk",
		"body":   body,
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectUpdate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectUpdate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created ProjectUpdateResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateProjectUpdate: %v", err)
	}
	if created.Health != "at_risk" {
		t.Errorf("created.Health = %q, want at_risk", created.Health)
	}
	if created.Body != body {
		t.Errorf("created.Body = %q, want %q", created.Body, body)
	}
	if created.ProjectID != project.ID {
		t.Errorf("created.ProjectID = %q, want %q", created.ProjectID, project.ID)
	}
	if created.AuthorType != "member" || created.AuthorID != testUserID {
		t.Errorf("created author = %s/%s, want member/%s", created.AuthorType, created.AuthorID, testUserID)
	}

	// List must return the update with the body present.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/updates", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectUpdates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectUpdates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Updates []ProjectUpdateResponse `json:"updates"`
		Total   int                     `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Updates) != 1 {
		t.Fatalf("list returned %d updates, want 1", listResp.Total)
	}
	if listResp.Updates[0].ID != created.ID {
		t.Errorf("list[0].ID = %q, want %q", listResp.Updates[0].ID, created.ID)
	}
	if listResp.Updates[0].Body != body {
		t.Errorf("list[0].Body = %q, want %q", listResp.Updates[0].Body, body)
	}
	if listResp.Updates[0].Health != "at_risk" {
		t.Errorf("list[0].Health = %q, want at_risk", listResp.Updates[0].Health)
	}

	// GET the project: derived health must equal the latest update's health.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode GetProject: %v", err)
	}
	if got.Health == nil {
		t.Fatalf("GetProject: derived health is nil, want at_risk")
	}
	if *got.Health != "at_risk" {
		t.Errorf("GetProject derived health = %q, want at_risk", *got.Health)
	}

	// A newer update must move the derived health, proving it tracks the latest.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/updates", map[string]any{
		"health": "on_track",
		"body":   "Recovered.",
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectUpdate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectUpdate (second): expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProject (after second update): %d %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode GetProject: %v", err)
	}
	if got.Health == nil || *got.Health != "on_track" {
		t.Errorf("GetProject derived health after second update = %v, want on_track", got.Health)
	}
}

// TestProjectUpdateInvalidHealthRejected pins the validation surface: any
// health value outside {on_track, at_risk, off_track} must 400 before a row is
// written.
func TestProjectUpdateInvalidHealthRejected(t *testing.T) {
	project := createProjectUpdateTestProject(t, "Project update invalid health")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/updates", map[string]any{
		"health": "green",
		"body":   "should be rejected",
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectUpdate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid health: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Nothing must have been persisted.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/updates", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectUpdates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectUpdates: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 0 {
		t.Errorf("invalid health should not persist a row, got total = %d", listResp.Total)
	}
}

// TestProjectUpdateWorkspaceIsolation confirms a caller acting as a different
// workspace cannot read another workspace's project updates: the project is
// not found in workspace B, so the list returns 404 via the project loader.
func TestProjectUpdateWorkspaceIsolation(t *testing.T) {
	ctx := context.Background()
	project := createProjectUpdateTestProject(t, "Project update isolation")

	// Seed an update so there is real data to (fail to) read cross-workspace.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/updates", map[string]any{
		"health": "off_track",
		"body":   "Workspace A only.",
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectUpdate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectUpdate: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Create workspace B with its own owner member.
	otherSlug := "proj-update-iso-" + uuid.NewString()[:8]
	var otherUserID, otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Isolation User', $1)
		RETURNING id
	`, otherSlug+"@multica.ai").Scan(&otherUserID); err != nil {
		t.Fatalf("create isolation user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Isolation Workspace', $1, 'cross-workspace isolation test', 'ISO')
		RETURNING id
	`, otherSlug).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create isolation workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, otherWorkspaceID, otherUserID); err != nil {
		t.Fatalf("create isolation member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	// List workspace A's project updates while acting as workspace B. The
	// project itself is not visible in workspace B, so the loader 404s before
	// any updates are returned.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/updates", nil)
	req.Header.Set("X-User-ID", otherUserID)
	req.Header.Set("X-Workspace-ID", otherWorkspaceID)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectUpdates(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace ListProjectUpdates: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Sanity: the owning workspace can still read its update.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/updates", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectUpdates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner ListProjectUpdates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode owner list: %v", err)
	}
	if listResp.Total != 1 {
		t.Errorf("owner list total = %d, want 1", listResp.Total)
	}
}
