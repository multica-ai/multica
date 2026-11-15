package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func archiveProjectForTest(t *testing.T, userID, projectID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, http.MethodPost, "/api/projects/"+projectID+"/archive", nil)
	testHandler.ArchiveProject(w, withURLParam(req, "id", projectID))
	return w
}

func restoreProjectForTest(t *testing.T, userID, projectID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, http.MethodPost, "/api/projects/"+projectID+"/restore", nil)
	testHandler.RestoreProject(w, withURLParam(req, "id", projectID))
	return w
}

func listProjectIDsForTest(t *testing.T, query string) map[string]bool {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest(http.MethodGet, "/api/projects?workspace_id="+testWorkspaceID+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ListProjects: %v", err)
	}
	ids := make(map[string]bool, len(resp.Projects))
	for _, p := range resp.Projects {
		ids[p.ID] = true
	}
	return ids
}

func createIssueForArchiveTest(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body))
	if w.Code == http.StatusCreated {
		var issue IssueResponse
		if err := json.Unmarshal(w.Body.Bytes(), &issue); err == nil {
			t.Cleanup(func() {
				_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
			})
		}
	}
	return w
}

func decodeErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

func TestArchiveAndRestoreProject(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Any member who can edit the project can archive it; deletion's
	// creator/admin gate does not apply.
	memberID := createProjectPermissionTestMember(t, "member")
	project := createProjectPermissionTestProject(t, "archive lifecycle project")

	w := archiveProjectForTest(t, memberID, project.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveProject: %d %s", w.Code, w.Body.String())
	}
	var archived ArchiveProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&archived); err != nil {
		t.Fatalf("decode ArchiveProject: %v", err)
	}
	if archived.ArchivedAt == nil || archived.ArchivedByType == nil || *archived.ArchivedByType != "member" ||
		archived.ArchivedByID == nil || *archived.ArchivedByID != memberID {
		t.Fatalf("archive fields = %v / %v / %v", archived.ArchivedAt, archived.ArchivedByType, archived.ArchivedByID)
	}
	if archived.Status != project.Status {
		t.Fatalf("archive changed status %q -> %q", project.Status, archived.Status)
	}

	if listProjectIDsForTest(t, "")[project.ID] {
		t.Fatal("default project list still returns the archived project")
	}
	if !listProjectIDsForTest(t, "&include_archived=true")[project.ID] {
		t.Fatal("include_archived=true does not return the archived project")
	}

	if w := archiveProjectForTest(t, memberID, project.ID); w.Code != http.StatusConflict {
		t.Fatalf("second archive: expected 409, got %d %s", w.Code, w.Body.String())
	}

	w = restoreProjectForTest(t, memberID, project.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("RestoreProject: %d %s", w.Code, w.Body.String())
	}
	var restored ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&restored); err != nil {
		t.Fatalf("decode RestoreProject: %v", err)
	}
	if restored.ArchivedAt != nil || restored.ArchivedByType != nil || restored.ArchivedByID != nil {
		t.Fatalf("restore left archive fields set: %+v", restored)
	}
	if !listProjectIDsForTest(t, "")[project.ID] {
		t.Fatal("restored project is missing from the default list")
	}
	if w := restoreProjectForTest(t, memberID, project.ID); w.Code != http.StatusConflict {
		t.Fatalf("second restore: expected 409, got %d %s", w.Code, w.Body.String())
	}
}

func TestArchiveProjectRejectsOtherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	foreignWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Foreign Archive WS",
		"slug":         "foreign-archive-ws",
		"description":  "cross-tenant archive test",
		"issue_prefix": "FAW",
	})
	foreignProjectID := dbfx.Insert(t, "project", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"title":        "foreign project",
	})

	if w := archiveProjectForTest(t, testUserID, foreignProjectID); w.Code != http.StatusNotFound {
		t.Fatalf("archive foreign project: expected 404, got %d %s", w.Code, w.Body.String())
	}
	var archived bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT archived_at IS NOT NULL FROM project WHERE id = $1`, foreignProjectID,
	).Scan(&archived); err != nil {
		t.Fatalf("read foreign project: %v", err)
	}
	if archived {
		t.Fatal("foreign project was archived through another workspace")
	}
}

func TestArchivedProjectRefusesNewIssuesButKeepsExistingOnesEditable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	project := createProjectPermissionTestProject(t, "archive guard project")

	w := createIssueForArchiveTest(t, map[string]any{"title": "existing work", "project_id": project.ID})
	if w.Code != http.StatusCreated {
		t.Fatalf("seed issue: %d %s", w.Code, w.Body.String())
	}
	var existing IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&existing); err != nil {
		t.Fatalf("decode seed issue: %v", err)
	}
	w = createIssueForArchiveTest(t, map[string]any{"title": "elsewhere"})
	if w.Code != http.StatusCreated {
		t.Fatalf("seed outside issue: %d %s", w.Code, w.Body.String())
	}
	var outside IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&outside); err != nil {
		t.Fatalf("decode outside issue: %v", err)
	}

	if w := archiveProjectForTest(t, testUserID, project.ID); w.Code != http.StatusOK {
		t.Fatalf("ArchiveProject: %d %s", w.Code, w.Body.String())
	}

	w = createIssueForArchiveTest(t, map[string]any{"title": "new work", "project_id": project.ID})
	if w.Code != http.StatusConflict || decodeErrorCode(t, w) != projectArchivedCode {
		t.Fatalf("create into archived project: expected 409 %s, got %d %s", projectArchivedCode, w.Code, w.Body.String())
	}

	// A sub-issue inherits its parent's project; continuing existing work is allowed.
	w = createIssueForArchiveTest(t, map[string]any{"title": "follow-up", "parent_issue_id": existing.ID})
	if w.Code != http.StatusCreated {
		t.Fatalf("sub-issue under archived project: expected 201, got %d %s", w.Code, w.Body.String())
	}
	// The create form names the parent's project explicitly; that is still the parent's project.
	w = createIssueForArchiveTest(t, map[string]any{"title": "follow-up 2", "parent_issue_id": existing.ID, "project_id": project.ID})
	if w.Code != http.StatusCreated {
		t.Fatalf("sub-issue naming its parent's archived project: expected 201, got %d %s", w.Code, w.Body.String())
	}
	w = createIssueForArchiveTest(t, map[string]any{"title": "stray", "parent_issue_id": outside.ID, "project_id": project.ID})
	if w.Code != http.StatusConflict || decodeErrorCode(t, w) != projectArchivedCode {
		t.Fatalf("sub-issue filed into another parent's archived project: expected 409, got %d %s", w.Code, w.Body.String())
	}

	update := func(issueID string, body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPut, "/api/issues/"+issueID, body)
		testHandler.UpdateIssue(w, withURLParam(req, "id", issueID))
		return w
	}
	if w := update(existing.ID, map[string]any{"title": "existing work, renamed", "project_id": project.ID}); w.Code != http.StatusOK {
		t.Fatalf("edit issue inside archived project: expected 200, got %d %s", w.Code, w.Body.String())
	}
	if w := update(outside.ID, map[string]any{"project_id": project.ID}); w.Code != http.StatusConflict || decodeErrorCode(t, w) != projectArchivedCode {
		t.Fatalf("move issue into archived project: expected 409, got %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{outside.ID},
		"updates":   map[string]any{"project_id": project.ID},
	}))
	if w.Code != http.StatusConflict || decodeErrorCode(t, w) != projectArchivedCode {
		t.Fatalf("batch move into archived project: expected 409, got %d %s", w.Code, w.Body.String())
	}

	if w := restoreProjectForTest(t, testUserID, project.ID); w.Code != http.StatusOK {
		t.Fatalf("RestoreProject: %d %s", w.Code, w.Body.String())
	}
	if w := update(outside.ID, map[string]any{"project_id": project.ID}); w.Code != http.StatusOK {
		t.Fatalf("move issue into restored project: expected 200, got %d %s", w.Code, w.Body.String())
	}
}

func TestArchiveProjectPausesAutopilotsAndBlocksResume(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	project := createProjectPermissionTestProject(t, "archive autopilot project")
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Archive Autopilot Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Archive Autopilot Agent")

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id, status,
			execution_mode, created_by_type, created_by_id, project_id
		)
		VALUES ($1, 'files into archived project', 'agent', $2, 'active',
			'create_issue', 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, project.ID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})

	w := archiveProjectForTest(t, testUserID, project.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveProject: %d %s", w.Code, w.Body.String())
	}
	var archived ArchiveProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&archived); err != nil {
		t.Fatalf("decode ArchiveProject: %v", err)
	}
	if len(archived.PausedAutopilotIDs) != 1 || archived.PausedAutopilotIDs[0] != autopilotID {
		t.Fatalf("paused_autopilot_ids = %v, want [%s]", archived.PausedAutopilotIDs, autopilotID)
	}

	readAutopilot := func() (string, string) {
		var status, reason string
		if err := testPool.QueryRow(ctx,
			`SELECT status, COALESCE(pause_reason, '') FROM autopilot WHERE id = $1`, autopilotID,
		).Scan(&status, &reason); err != nil {
			t.Fatalf("read autopilot: %v", err)
		}
		return status, reason
	}
	if status, reason := readAutopilot(); status != "paused" || reason != "project_archived" {
		t.Fatalf("autopilot = (%q, %q), want (paused, project_archived)", status, reason)
	}

	resume := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID,
			map[string]any{"status": "active"})
		testHandler.UpdateAutopilot(w, withURLParam(req, "id", autopilotID))
		return w
	}
	if w := resume(); w.Code != http.StatusConflict || decodeErrorCode(t, w) != projectArchivedCode {
		t.Fatalf("resume autopilot on archived project: expected 409, got %d %s", w.Code, w.Body.String())
	}

	// Restoring does not resume automation on its own.
	if w := restoreProjectForTest(t, testUserID, project.ID); w.Code != http.StatusOK {
		t.Fatalf("RestoreProject: %d %s", w.Code, w.Body.String())
	}
	if status, _ := readAutopilot(); status != "paused" {
		t.Fatalf("restore resumed the autopilot: status %q", status)
	}
	if w := resume(); w.Code != http.StatusOK {
		t.Fatalf("resume after restore: expected 200, got %d %s", w.Code, w.Body.String())
	}
}

func TestArchivedProjectStillCountsAsWorkflowUser(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	wf := createTestWorkflow(t, "Archived user "+workflowTestSuffix(), "in_progress")
	projectID := createWorkflowTestProject(t, "Archived workflow project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)

	projectIDs := func() []string {
		t.Helper()
		var list struct {
			Workflows []IssueWorkflowResponse `json:"workflows"`
		}
		testutil.Call(t, testHandler.ListIssueWorkflows, workflowRequest("GET", "/api/issue-workflows", nil)).Want(http.StatusOK).JSON(&list)
		for _, row := range list.Workflows {
			if row.ID == wf.ID {
				return row.ProjectIDs
			}
		}
		t.Fatalf("workflow %s missing from list", wf.ID)
		return nil
	}
	if got := projectIDs(); len(got) != 1 || got[0] != projectID {
		t.Fatalf("before archive: project_ids = %v, want [%s]", got, projectID)
	}

	if w := archiveProjectForTest(t, testUserID, projectID); w.Code != http.StatusOK {
		t.Fatalf("ArchiveProject: %d %s", w.Code, w.Body.String())
	}
	if got := projectIDs(); len(got) != 1 || got[0] != projectID {
		t.Fatalf("after archive: project_ids = %v, want the archived project still listed", got)
	}
}
