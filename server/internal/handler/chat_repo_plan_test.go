package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newWorkspaceRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()

	req := newRequest(method, path, body)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("setup: load member: %v", err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}

func createHandlerTestChatTask(t *testing.T, status string) (sessionID string, taskID string) {
	t.Helper()

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id, runtime_id FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: load agent/runtime: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'repo-plan regression')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("setup: create chat session: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, chat_session_id
		)
		VALUES ($1, $2, NULL, $3, 2, $4)
		RETURNING id
	`, agentID, runtimeID, status, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create chat task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	return sessionID, taskID
}

func TestListPendingChatTasks_IncludesAwaitingUser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, taskID := createHandlerTestChatTask(t, "awaiting_user")

	w := httptest.NewRecorder()
	req := newWorkspaceRequest(t, http.MethodGet, "/api/chat/pending-tasks", nil)

	testHandler.ListPendingChatTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListPendingChatTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp PendingChatTasksResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, item := range resp.Tasks {
		if item.TaskID == taskID && item.Status == "awaiting_user" {
			return
		}
	}
	t.Fatalf("expected awaiting_user task %s in pending aggregate, got %#v", taskID, resp.Tasks)
}

func TestCancelTaskByUser_CancelsAwaitingUserChatTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, taskID := createHandlerTestChatTask(t, "awaiting_user")

	w := httptest.NewRecorder()
	req := withURLParam(newWorkspaceRequest(t, http.MethodPost, "/api/tasks/"+taskID+"/cancel", nil), "taskId", taskID)

	testHandler.CancelTaskByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CancelTaskByUser: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("expected task status cancelled, got %q", status)
	}
}
