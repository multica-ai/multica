package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createProjectFileTestProject(t *testing.T, title string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject(%q): %d %s", title, w.Code, w.Body.String())
	}
	var p ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+p.ID, nil)
		r = withURLParam(r, "id", p.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	return p
}

func createProjectFileTestIssue(t *testing.T, projectID string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues", map[string]any{
		"title":      "project file test issue",
		"status":     "backlog",
		"priority":   "none",
		"project_id": projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode CreateIssue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

func insertTestAttachment(t *testing.T, issueID, commentID, chatSessionID string, uploaderType string, uploaderID string, filename string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (
			workspace_id, issue_id, comment_id, chat_session_id,
			uploader_type, uploader_id, filename, url, content_type, size_bytes
		)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid,
			$5, $6, $7, 'http://example.invalid/' || $7, 'application/octet-stream', 42)
		RETURNING id
	`, testWorkspaceID, issueID, commentID, chatSessionID, uploaderType, uploaderID, filename).Scan(&id)
	if err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return id
}

func listProjectFiles(t *testing.T, projectID string) (files []ProjectFileResponse, total int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID+"/files", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ListProjectFiles(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectFiles: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Files []ProjectFileResponse `json:"files"`
		Total int                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ListProjectFiles: %v", err)
	}
	return resp.Files, resp.Total
}

func hideProjectFile(t *testing.T, projectID, attachmentID string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/files/"+attachmentID+"/hide", nil)
	req = withURLParams(req, "id", projectID, "attachmentId", attachmentID)
	testHandler.HideProjectFile(w, req)
	return w.Code
}

func unhideProjectFile(t *testing.T, projectID, attachmentID string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/projects/"+projectID+"/files/"+attachmentID+"/hide", nil)
	req = withURLParams(req, "id", projectID, "attachmentId", attachmentID)
	testHandler.UnhideProjectFile(w, req)
	return w.Code
}

func TestProjectFilesListAndHideLifecycle(t *testing.T) {
	project := createProjectFileTestProject(t, "project files lifecycle")
	issue := createProjectFileTestIssue(t, project.ID)
	attachmentID := insertTestAttachment(t, issue.ID, "", "", "member", testUserID, "report.pdf")

	files, total := listProjectFiles(t, project.ID)
	if total != 1 || len(files) != 1 {
		t.Fatalf("initial list: total=%d files=%d, want 1/1", total, len(files))
	}
	if files[0].ID != attachmentID {
		t.Errorf("list[0].ID = %q, want %q", files[0].ID, attachmentID)
	}
	if files[0].Hidden {
		t.Errorf("fresh file should not be hidden")
	}
	if files[0].UploaderType != "member" || files[0].UploaderID != testUserID {
		t.Errorf("uploader = %s/%s, want member/%s", files[0].UploaderType, files[0].UploaderID, testUserID)
	}

	// Hide → listed with hidden=true.
	if code := hideProjectFile(t, project.ID, attachmentID); code != http.StatusNoContent {
		t.Fatalf("hide: %d", code)
	}
	files, _ = listProjectFiles(t, project.ID)
	if len(files) != 1 || !files[0].Hidden {
		t.Fatalf("after hide: files=%+v, want one hidden file", files)
	}

	// Re-hide is idempotent.
	if code := hideProjectFile(t, project.ID, attachmentID); code != http.StatusNoContent {
		t.Fatalf("re-hide: %d", code)
	}

	// Unhide → visible again.
	if code := unhideProjectFile(t, project.ID, attachmentID); code != http.StatusNoContent {
		t.Fatalf("unhide: %d", code)
	}
	files, _ = listProjectFiles(t, project.ID)
	if len(files) != 1 || files[0].Hidden {
		t.Fatalf("after unhide: files=%+v, want one visible file", files)
	}

	// Unhide again is idempotent.
	if code := unhideProjectFile(t, project.ID, attachmentID); code != http.StatusNoContent {
		t.Fatalf("re-unhide: %d", code)
	}
}

// TestProjectFilesScopesAcrossIssueCommentChat pins the aggregation contract:
// a file surfaces when it is attached to the project through any of the three
// project-bearing paths (issue, comment→issue, chat session). Attachments on
// unrelated issues are excluded.
func TestProjectFilesScopesAcrossIssueCommentChat(t *testing.T) {
	project := createProjectFileTestProject(t, "project files scoping")
	issue := createProjectFileTestIssue(t, project.ID)

	// A second project + issue in the same workspace must NOT leak into the
	// first project's list.
	otherProject := createProjectFileTestProject(t, "project files other")
	otherIssue := createProjectFileTestIssue(t, otherProject.ID)
	insertTestAttachment(t, otherIssue.ID, "", "", "member", testUserID, "foreign.txt")

	// Issue-scoped file.
	issueAtt := insertTestAttachment(t, issue.ID, "", "", "agent", testUserID, "issue.txt")

	// Comment-scoped file: a comment on the project's issue.
	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'produced a file')
		RETURNING id
	`, testWorkspaceID, issue.ID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})
	insertTestAttachment(t, "", commentID, "", "agent", testUserID, "comment.txt")

	// Chat-scoped file: a chat session linked to the project.
	agentID := createHandlerTestAgent(t, "project files agent", nil)
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, project_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, project.ID).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat_session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	insertTestAttachment(t, "", "", sessionID, "agent", agentID, "chat.txt")

	files, total := listProjectFiles(t, project.ID)
	if total != 3 || len(files) != 3 {
		t.Fatalf("scoped list: total=%d files=%d, want 3/3 (got %+v)", total, len(files), files)
	}
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Filename] = true
	}
	for _, name := range []string{"issue.txt", "comment.txt", "chat.txt"} {
		if !seen[name] {
			t.Errorf("missing %s in project files: %+v", name, files)
		}
	}
	if seen["foreign.txt"] {
		t.Errorf("foreign attachment leaked into project list: %+v", files)
	}
	if issueAtt == "" {
		t.Errorf("issue attachment id should be non-empty")
	}
}

// TestProjectFilesHideRejectsForeignAttachment pins the write gate: a member
// cannot hide a file that belongs to another project, and a nonexistent
// attachment id is indistinguishable from a foreign one (404).
func TestProjectFilesHideRejectsForeignAttachment(t *testing.T) {
	projectA := createProjectFileTestProject(t, "project files gate A")
	projectB := createProjectFileTestProject(t, "project files gate B")
	issueA := createProjectFileTestIssue(t, projectA.ID)
	attachmentID := insertTestAttachment(t, issueA.ID, "", "", "member", testUserID, "secret.txt")

	if code := hideProjectFile(t, projectB.ID, attachmentID); code != http.StatusNotFound {
		t.Errorf("foreign hide: expected 404, got %d", code)
	}
	if code := unhideProjectFile(t, projectB.ID, attachmentID); code != http.StatusNotFound {
		t.Errorf("foreign unhide: expected 404, got %d", code)
	}
	if code := hideProjectFile(t, projectA.ID, "00000000-0000-0000-0000-000000000000"); code != http.StatusNotFound {
		t.Errorf("missing attachment hide: expected 404, got %d", code)
	}
}
