package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// seedRunningChatTask inserts a running chat task (chat_session_id set) for the
// given agent and returns its id. Mirrors createHandlerTestTaskForAgentOnIssue
// but binds a chat session instead of an issue.
func seedRunningChatTask(t *testing.T, agentID, sessionID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id, started_at)
		VALUES ($1, $2, 'running', 0, $3, now())
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), sessionID).Scan(&taskID); err != nil {
		t.Fatalf("seed running chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// seedAgentChatAttachment inserts an attachment as if the agent had uploaded it
// during a chat task: tagged with task_id + chat_session_id, no owner message
// yet. Returns the attachment id.
func seedAgentChatAttachment(t *testing.T, agentID, sessionID, taskID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, task_id, chat_session_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, $2, $3, 'agent', $4, 'chart.png', 'https://cdn.example/chart.png', 'image/png', 123)
		RETURNING id
	`, testWorkspaceID, taskID, sessionID, agentID).Scan(&id); err != nil {
		t.Fatalf("seed agent chat attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return id
}

func attachmentMessageID(t *testing.T, attachmentID string) *string {
	t.Helper()
	var msg *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`, attachmentID).Scan(&msg); err != nil {
		t.Fatalf("query attachment chat_message_id: %v", err)
	}
	return msg
}

func assistantMessageForTask(t *testing.T, taskID string) (id, content string, ok bool) {
	t.Helper()
	err := testPool.QueryRow(context.Background(),
		`SELECT id::text, content FROM chat_message WHERE task_id = $1 AND role = 'assistant'`, taskID).Scan(&id, &content)
	if err != nil {
		return "", "", false
	}
	return id, content, true
}

// doUpload performs a multipart upload of a task_id form field with the given
// request headers, and returns the recorder.
func doUpload(t *testing.T, formTaskID string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "reply.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	if err := writer.WriteField("task_id", formTaskID); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	return w
}

// uploadWithTaskID performs a multipart upload as a genuine task-token agent
// request: it stamps the server-set X-Actor-Source=task_token + X-Task-ID pair
// exactly as the auth middleware would for a `mat_` token (the boundary the
// handler trusts). actorTaskID is what goes on X-Task-ID (the token's task);
// formTaskID is the upload's task_id form field. They match in the happy path.
// When agentID is empty the caller is a plain member (no task-token headers).
func uploadWithTaskID(t *testing.T, agentID, actorTaskID, formTaskID string) *httptest.ResponseRecorder {
	t.Helper()
	headers := map[string]string{
		"X-User-ID":      testUserID,
		"X-Workspace-ID": testWorkspaceID,
	}
	if agentID != "" {
		// Mirror the middleware: a task token stamps the actor source and task.
		headers["X-Actor-Source"] = "task_token"
		headers["X-Agent-ID"] = agentID
	}
	if actorTaskID != "" {
		headers["X-Task-ID"] = actorTaskID
	}
	return doUpload(t, formTaskID, headers)
}

// TestUploadFile_TaskScopedChatAttachment covers the write side: an agent
// uploading a file for its chat reply gets a row tagged with task_id +
// chat_session_id, and the permission/isolation gates reject the bad cases.
func TestUploadFile_TaskScopedChatAttachment(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	agentID := createHandlerTestAgent(t, "ChatReplyAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := seedRunningChatTask(t, agentID, sessionID)

	t.Run("agent uploads for own chat task", func(t *testing.T) {
		w := uploadWithTaskID(t, agentID, taskID, taskID)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp AttachmentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v; body: %s", err, w.Body.String())
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, resp.ID)
		})
		if resp.ChatSessionID == nil || *resp.ChatSessionID != sessionID {
			t.Fatalf("chat_session_id: want %s, got %v", sessionID, resp.ChatSessionID)
		}
		if resp.ChatMessageID != nil {
			t.Fatalf("chat_message_id must be NULL before completion, got %v", resp.ChatMessageID)
		}
		if resp.UploaderType != "agent" {
			t.Fatalf("uploader_type: want agent, got %s", resp.UploaderType)
		}
		// task_id is set on the row (not exposed in the DTO — verify via DB).
		var dbTask *string
		if err := testPool.QueryRow(context.Background(),
			`SELECT task_id::text FROM attachment WHERE id = $1`, resp.ID).Scan(&dbTask); err != nil {
			t.Fatalf("query task_id: %v", err)
		}
		if dbTask == nil || *dbTask != taskID {
			t.Fatalf("task_id: want %s, got %v", taskID, dbTask)
		}
	})

	t.Run("member actor rejected", func(t *testing.T) {
		// A plain member request (no task-token headers) is rejected by the
		// task-token boundary before any task lookup.
		w := uploadWithTaskID(t, "", "", taskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("member upload: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("forged agent headers without task token rejected", func(t *testing.T) {
		// The real forgery vector: a normal JWT / mul_ PAT request that the auth
		// middleware did NOT stamp with X-Actor-Source=task_token, but which
		// forges a valid X-Agent-ID + X-Task-ID pair (resolveActor's fallback
		// would otherwise accept it). Even with the form task_id equal to the
		// forged X-Task-ID, the missing task-token source must make it 403 —
		// otherwise a member who learns a task ID could inject an attachment
		// into that task's chat reply.
		w := doUpload(t, taskID, map[string]string{
			"X-User-ID":      testUserID,
			"X-Workspace-ID": testWorkspaceID,
			"X-Agent-ID":     agentID,
			"X-Task-ID":      taskID,
			// deliberately NO X-Actor-Source
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("forged non-task-token upload: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("different agent's task rejected", func(t *testing.T) {
		otherAgent := createHandlerTestAgent(t, "OtherReplyAgent", []byte("[]"))
		otherTask := createHandlerTestTaskForAgent(t, otherAgent)
		// Actor is agentID (valid X-Agent-ID/X-Task-ID pair), but the upload
		// targets a task owned by a different agent.
		w := uploadWithTaskID(t, agentID, taskID, otherTask)
		if w.Code != http.StatusForbidden {
			t.Fatalf("foreign task upload: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("same agent's other chat task rejected", func(t *testing.T) {
		// X-Task-ID is this run's own task, but the form targets a DIFFERENT
		// chat task of the SAME agent (another session, possibly another user).
		// Without pinning the form task_id to the token's bound X-Task-ID this
		// is a cross-session attachment-injection vector.
		otherSession := createHandlerTestChatSession(t, agentID)
		otherChatTask := seedRunningChatTask(t, agentID, otherSession)
		w := uploadWithTaskID(t, agentID, taskID, otherChatTask)
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-task upload: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-chat task rejected", func(t *testing.T) {
		issueTask := createHandlerTestTaskForAgent(t, agentID) // no chat_session_id
		w := uploadWithTaskID(t, agentID, issueTask, issueTask)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("non-chat task upload: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("malformed task_id rejected", func(t *testing.T) {
		w := uploadWithTaskID(t, agentID, taskID, "not-a-uuid")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad task_id: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestChatAttachment_UnboundOrphanReapedOnSessionDelete locks the cleanup
// guarantee that lets us keep task_id as a plain (FK-less) transient column: an
// unbound task-tagged upload (task_id set, chat_message_id NULL — e.g. the turn
// failed before binding) is reaped when its chat_session is deleted, via
// attachment.chat_session_id's ON DELETE CASCADE. Cleanup does not depend on the
// task relationship, so no attachment.task_id foreign key / cascade is needed.
func TestChatAttachment_UnboundOrphanReapedOnSessionDelete(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	agentID := createHandlerTestAgent(t, "OrphanCleanupAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := seedRunningChatTask(t, agentID, sessionID)
	attID := seedAgentChatAttachment(t, agentID, sessionID, taskID)

	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM chat_session WHERE id = $1`, sessionID); err != nil {
		t.Fatalf("delete chat session: %v", err)
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM attachment WHERE id = $1)`, attID).Scan(&exists); err != nil {
		t.Fatalf("check attachment: %v", err)
	}
	if exists {
		t.Fatal("unbound task-tagged attachment must be reaped when its chat_session is deleted")
	}
}

// TestCompleteTask_BindsChatAttachments covers the read/bind side: on chat task
// completion the agent's task-scoped attachments bind to the assistant reply.
func TestCompleteTask_BindsChatAttachments(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	agentID := createHandlerTestAgent(t, "BindReplyAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	t.Run("output + attachment binds to reply", func(t *testing.T) {
		taskID := seedRunningChatTask(t, agentID, sessionID)
		attID := seedAgentChatAttachment(t, agentID, sessionID, taskID)

		if _, err := testHandler.TaskService.CompleteTask(context.Background(),
			parseUUID(taskID), []byte(`{"output":"here is the chart"}`), "", ""); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
		msgID, content, ok := assistantMessageForTask(t, taskID)
		if !ok {
			t.Fatal("expected an assistant message to be created")
		}
		if content != "here is the chart" {
			t.Fatalf("content: want %q, got %q", "here is the chart", content)
		}
		got := attachmentMessageID(t, attID)
		if got == nil || *got != msgID {
			t.Fatalf("attachment not bound: want message %s, got %v", msgID, got)
		}
	})

	t.Run("empty output + attachment still creates message and binds", func(t *testing.T) {
		taskID := seedRunningChatTask(t, agentID, sessionID)
		attID := seedAgentChatAttachment(t, agentID, sessionID, taskID)

		if _, err := testHandler.TaskService.CompleteTask(context.Background(),
			parseUUID(taskID), []byte(`{"output":""}`), "", ""); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
		msgID, content, ok := assistantMessageForTask(t, taskID)
		if !ok {
			t.Fatal("image-only reply must still create an assistant message")
		}
		if content != "" {
			t.Fatalf("content should be empty for image-only reply, got %q", content)
		}
		got := attachmentMessageID(t, attID)
		if got == nil || *got != msgID {
			t.Fatalf("attachment not bound: want message %s, got %v", msgID, got)
		}
	})

	t.Run("empty output + no attachment creates no message", func(t *testing.T) {
		taskID := seedRunningChatTask(t, agentID, sessionID)
		if _, err := testHandler.TaskService.CompleteTask(context.Background(),
			parseUUID(taskID), []byte(`{"output":""}`), "", ""); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
		if _, _, ok := assistantMessageForTask(t, taskID); ok {
			t.Fatal("no output and no attachments must not create an assistant message")
		}
	})

	t.Run("null task_id attachment in same session is not bound", func(t *testing.T) {
		taskID := seedRunningChatTask(t, agentID, sessionID)
		// A loose session attachment with NO task_id (e.g. legacy row) must be
		// left alone — binding is scoped to the producing task.
		var looseID string
		if err := testPool.QueryRow(context.Background(), `
			INSERT INTO attachment (workspace_id, chat_session_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
			VALUES ($1, $2, 'agent', $3, 'loose.png', 'https://cdn.example/loose.png', 'image/png', 10)
			RETURNING id
		`, testWorkspaceID, sessionID, agentID).Scan(&looseID); err != nil {
			t.Fatalf("seed loose attachment: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, looseID) })

		if _, err := testHandler.TaskService.CompleteTask(context.Background(),
			parseUUID(taskID), []byte(`{"output":"done"}`), "", ""); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
		if got := attachmentMessageID(t, looseID); got != nil {
			t.Fatalf("null-task_id attachment must not be bound, got %v", got)
		}
	})

	t.Run("already-owned attachment is not stolen", func(t *testing.T) {
		// The bind's WHERE guards are symmetric: comment_id IS NULL AND
		// issue_id IS NULL AND chat_message_id IS NULL. We exercise the guard
		// via an already-bound (chat_message_id set) row with this task's
		// task_id — proving an attachment already claimed by another owner is
		// never re-pointed at the new reply.
		taskID := seedRunningChatTask(t, agentID, sessionID)
		var claimedID string
		if err := testPool.QueryRow(context.Background(), `
			INSERT INTO attachment (workspace_id, task_id, chat_session_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
			VALUES ($1, $2, $3, 'agent', $4, 'claimed.png', 'https://cdn.example/claimed.png', 'image/png', 10)
			RETURNING id
		`, testWorkspaceID, taskID, sessionID, agentID).Scan(&claimedID); err != nil {
			t.Fatalf("seed attachment: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, claimedID) })
		var priorMsgID string
		if err := testPool.QueryRow(context.Background(), `
			INSERT INTO chat_message (chat_session_id, role, content)
			VALUES ($1, 'assistant', 'prior') RETURNING id
		`, sessionID).Scan(&priorMsgID); err != nil {
			t.Fatalf("seed prior message: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_message WHERE id = $1`, priorMsgID) })
		if _, err := testPool.Exec(context.Background(),
			`UPDATE attachment SET chat_message_id = $1 WHERE id = $2`, priorMsgID, claimedID); err != nil {
			t.Fatalf("pre-bind attachment: %v", err)
		}

		if _, err := testHandler.TaskService.CompleteTask(context.Background(),
			parseUUID(taskID), []byte(`{"output":"done"}`), "", ""); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
		got := attachmentMessageID(t, claimedID)
		if got == nil || *got != priorMsgID {
			t.Fatalf("already-bound attachment must keep its owner %s, got %v", priorMsgID, got)
		}
	})

	t.Run("FailTask does not bind attachments", func(t *testing.T) {
		taskID := seedRunningChatTask(t, agentID, sessionID)
		attID := seedAgentChatAttachment(t, agentID, sessionID, taskID)
		if _, err := testHandler.TaskService.FailTask(context.Background(),
			parseUUID(taskID), "agent crashed", "", "", ""); err != nil {
			t.Fatalf("FailTask: %v", err)
		}
		if got := attachmentMessageID(t, attID); got != nil {
			t.Fatalf("FailTask must not bind attachments, got %v", got)
		}
	})
}
