package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var worklogSchemaOnce sync.Once

func ensureWorklogSchema(t *testing.T) {
	t.Helper()

	worklogSchemaOnce.Do(func() {
		_, err := testPool.Exec(context.Background(), `
			CREATE TABLE IF NOT EXISTS worklog (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
				author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
				author_id UUID NOT NULL,
				duration_minutes INT NOT NULL CHECK (duration_minutes > 0),
				description TEXT,
				type TEXT NOT NULL DEFAULT 'manual' CHECK (type IN ('manual', 'pomodoro')),
				logged_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
			);

			CREATE TABLE IF NOT EXISTS worklog_issue (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				worklog_id UUID NOT NULL REFERENCES worklog(id) ON DELETE CASCADE,
				issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
				workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				UNIQUE (worklog_id, issue_id)
			);

			CREATE INDEX IF NOT EXISTS idx_worklog_workspace_logged_at ON worklog (workspace_id, logged_at DESC);
			CREATE INDEX IF NOT EXISTS idx_worklog_issue_issue_workspace ON worklog_issue (issue_id, workspace_id, created_at DESC);
		`)
		if err != nil {
			t.Fatalf("ensure worklog schema: %v", err)
		}
	})
}

func createWorklogTestIssue(t *testing.T, title string) string {
	t.Helper()
	ensureWorklogSchema(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `DELETE FROM worklog WHERE id IN (SELECT worklog_id FROM worklog_issue WHERE issue_id = $1)`, issue.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	return issue.ID
}

func createWorklogEntry(t *testing.T, issueID string, durationMinutes int32, description string) WorklogResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/worklogs", map[string]any{
		"duration_minutes": durationMinutes,
		"description":      description,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateWorklog(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorklog: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var worklog WorklogResponse
	if err := json.NewDecoder(w.Body).Decode(&worklog); err != nil {
		t.Fatalf("decode worklog: %v", err)
	}

	return worklog
}

func newRequestAsUser(method, path string, body any, userID string) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	return req
}

func createSecondaryWorkspaceMember(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	email := fmt.Sprintf("worklog-test-%d@multica.ai", time.Now().UnixNano())
	name := fmt.Sprintf("Worklog Test %d", time.Now().UnixNano())

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("create secondary user: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("create secondary member: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

func TestCreateWorklog(t *testing.T) {
	issueID := createWorklogTestIssue(t, "Worklog create issue")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/worklogs", map[string]any{
		"duration_minutes": 90,
		"description":      "Investigated the failure and fixed it",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateWorklog(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorklog: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created WorklogResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created worklog: %v", err)
	}
	if created.IssueID != issueID {
		t.Fatalf("CreateWorklog: expected issue_id %q, got %q", issueID, created.IssueID)
	}
	if created.DurationMinutes != 90 {
		t.Fatalf("CreateWorklog: expected duration 90, got %d", created.DurationMinutes)
	}
	if created.Description == nil || *created.Description != "Investigated the failure and fixed it" {
		t.Fatalf("CreateWorklog: unexpected description %#v", created.Description)
	}
	if created.AuthorType != "member" || created.AuthorID != testUserID {
		t.Fatalf("CreateWorklog: unexpected author %s/%s", created.AuthorType, created.AuthorID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/worklogs", map[string]any{
		"duration_minutes": 0,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateWorklog(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateWorklog invalid duration: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListWorklogsReturnsIssueEntries(t *testing.T) {
	issueID := createWorklogTestIssue(t, "Worklog list target")
	otherIssueID := createWorklogTestIssue(t, "Worklog list other")

	first := createWorklogEntry(t, issueID, 25, "First issue entry")
	createWorklogEntry(t, otherIssueID, 15, "Other issue entry")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/worklogs", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListWorklogs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorklogs: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var worklogs []WorklogResponse
	if err := json.NewDecoder(w.Body).Decode(&worklogs); err != nil {
		t.Fatalf("decode worklogs: %v", err)
	}
	if len(worklogs) != 1 {
		t.Fatalf("ListWorklogs: expected 1 worklog, got %d", len(worklogs))
	}
	if worklogs[0].ID != first.ID || worklogs[0].IssueID != issueID {
		t.Fatalf("ListWorklogs: unexpected worklog %+v", worklogs[0])
	}
}

func TestUpdateWorklog(t *testing.T) {
	issueID := createWorklogTestIssue(t, "Worklog update issue")
	created := createWorklogEntry(t, issueID, 30, "Initial work")

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/worklogs/"+created.ID, map[string]any{
		"duration_minutes": 45,
		"description":      "Expanded investigation and fix",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateWorklog(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWorklog: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated WorklogResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated worklog: %v", err)
	}
	if updated.DurationMinutes != 45 {
		t.Fatalf("UpdateWorklog: expected duration 45, got %d", updated.DurationMinutes)
	}
	if updated.Description == nil || *updated.Description != "Expanded investigation and fix" {
		t.Fatalf("UpdateWorklog: unexpected description %#v", updated.Description)
	}
}

func TestDeleteWorklogOwnership(t *testing.T) {
	issueID := createWorklogTestIssue(t, "Worklog delete issue")
	created := createWorklogEntry(t, issueID, 20, "Owned work")
	otherUserID := createSecondaryWorkspaceMember(t)

	w := httptest.NewRecorder()
	req := newRequestAsUser("DELETE", "/api/worklogs/"+created.ID, nil, otherUserID)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteWorklog(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteWorklog non-author: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/worklogs/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteWorklog(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorklog author: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/worklogs", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListWorklogs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorklogs after delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var remaining []WorklogResponse
	if err := json.NewDecoder(w.Body).Decode(&remaining); err != nil {
		t.Fatalf("decode remaining worklogs: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("DeleteWorklog: expected 0 remaining worklogs, got %d", len(remaining))
	}
}