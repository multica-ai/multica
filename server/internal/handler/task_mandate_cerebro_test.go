package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGetTaskMandateByUserReturnsExactStoredSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "task-access-snapshot", []byte(`{}`))
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position)
		VALUES ($1, 'task access snapshot', 'todo', 'none', 'member', $2, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO cerebro_task_mandate (task_id, workspace_id, agent_id, allowed_tools, expires_at)
		VALUES ($1,$2,$3,'["tools:Read","firtal_registry"]'::jsonb,$4)
	`, taskID, testWorkspaceID, agentID, expiresAt); err != nil {
		t.Fatalf("insert task mandate: %v", err)
	}

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: util.MustParseUUID(testUserID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	req := newRequest(http.MethodGet, "/api/tasks/"+taskID+"/access", nil)
	req = withURLParam(req, "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
	w := httptest.NewRecorder()

	testHandler.GetTaskMandateByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response taskMandateResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.TaskID != taskID || response.AgentID != agentID {
		t.Fatalf("identity = task %q agent %q, want %q / %q", response.TaskID, response.AgentID, taskID, agentID)
	}
	if len(response.AllowedTools) != 2 || response.AllowedTools[0] != "tools:Read" {
		t.Fatalf("allowed tools = %#v, want exact snapshot", response.AllowedTools)
	}
	if response.Status != "active" {
		t.Fatalf("status = %q, want active", response.Status)
	}
}

func TestGetTaskMandateByUserRejectsInvalidTaskID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/not-a-task/access", nil)
	req = withURLParam(req, "taskId", "not-a-task")
	(&Handler{}).GetTaskMandateByUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
