package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CancelTaskByUser (POST /api/tasks/{taskId}/cancel) used to key cancellation
// off issue_id / chat_session_id alone, which 404'd every task whose only
// source link was autopilot_run_id or quick_create context (MUL-2827). These
// tests pin the new behavior: tenancy flows through the task's owning agent,
// with chat-creator privacy and the private-agent visibility gate layered on.

// taskStatus reads a task's current status straight from the DB so reject
// paths can assert "no side effect before the access check".
func taskStatus(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	return status
}

// createAutopilotRunOnlyTask seeds the autopilot -> autopilot_run -> task chain
// that AutopilotService.dispatchRunOnly produces: a queued task with issue_id
// and chat_session_id NULL, linked only by autopilot_run_id. The autopilot is
// created in the agent's own workspace so the fixture works for foreign agents
// too.
func createAutopilotRunOnlyTask(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT workspace_id, runtime_id FROM agent WHERE id = $1`, agentID,
	).Scan(&workspaceID, &runtimeID); err != nil {
		t.Fatalf("load agent workspace/runtime: %v", err)
	}

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'cancel-runonly-ap', $2, 'run_only', 'member', $3)
		RETURNING id
	`, workspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("create autopilot_run: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, autopilot_run_id)
		VALUES ($1, $2, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, runID).Scan(&taskID); err != nil {
		t.Fatalf("create run_only task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// createForeignWorkspaceAgent stands up an isolated workspace + runtime + agent
// and returns the agent ID, for cross-tenant cancel tests.
func createForeignWorkspaceAgent(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Foreign Cancel WS', 'foreign-cancel-ws', 'cross-tenant cancel test', 'FCW')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Foreign Cancel Runtime', 'cloud', 'foreign_runtime', 'online', 'Foreign runtime', '{}'::jsonb, now())
		RETURNING id
	`, workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
		VALUES ($1, 'Foreign Cancel Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}
	return agentID
}

// createWorkspaceMemberUser adds a plain (non-owner/admin) member to the test
// workspace and returns the user ID. The member row cascades when the user is
// deleted (member.user_id ON DELETE CASCADE).
func createWorkspaceMemberUser(t *testing.T, name, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, name, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

func cancelTaskByUserRequest(t *testing.T, userID, taskID string) *http.Request {
	t.Helper()
	req := newRequestAs(userID, "POST", "/api/tasks/"+taskID+"/cancel", nil)
	req = withURLParam(req, "taskId", taskID)
	return withChatTestWorkspaceCtx(t, req)
}

func cancelQueuedTaskByUserRequest(
	t *testing.T,
	userID, taskID, sessionID, action string,
) *http.Request {
	t.Helper()
	req := newRequestAs(
		userID,
		http.MethodPost,
		"/api/tasks/"+taskID+"/cancel?expected_status=queued&chat_session_id="+
			sessionID+"&queue_action="+action,
		nil,
	)
	req = withURLParam(req, "taskId", taskID)
	return withChatTestWorkspaceCtx(t, req)
}

func TestCancelTaskByUser_QueuedOnlyDoesNotCancelPromotedTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelQueuedOnlyAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := insertPendingChatTask(t, agentID, sessionID, "running")
	req := newRequestAs(
		testUserID,
		http.MethodPost,
		"/api/tasks/"+taskID+"/cancel?expected_status=queued&chat_session_id="+sessionID+"&queue_action=remove",
		nil,
	)
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, withChatTestWorkspaceCtx(t, req))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "running" {
		t.Fatalf("queued-only cancellation changed promoted task status to %q", got)
	}
}

func TestCancelTaskByUser_QueuedEditPersistsDraftRestore(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "QueuedEditRestoreAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID, messageID, attachmentID := insertQueuedChatInputWithAttachment(
		t,
		agentID,
		sessionID,
		"edit this queued prompt",
	)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(
		w,
		cancelQueuedTaskByUserRequest(t, testUserID, taskID, sessionID, "edit"),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("queued edit task status = %q, want cancelled", got)
	}

	var restoreCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM chat_draft_restore
		WHERE id = $1
		  AND chat_session_id = $2
		  AND task_id = $3
		  AND content = 'edit this queued prompt'
		  AND $4::uuid = ANY(attachment_ids)
	`, messageID, sessionID, taskID, attachmentID).Scan(&restoreCount); err != nil {
		t.Fatalf("read durable queued edit restore: %v", err)
	}
	if restoreCount != 1 {
		t.Fatalf("queued edit created %d durable restore rows", restoreCount)
	}

	var messageCount int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`,
		messageID,
	).Scan(&messageCount); err != nil {
		t.Fatalf("count edited queued message: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("queued edit left %d input messages", messageCount)
	}
	var attachmentMessageID *string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`,
		attachmentID,
	).Scan(&attachmentMessageID); err != nil {
		t.Fatalf("read detached queued attachment: %v", err)
	}
	if attachmentMessageID != nil {
		t.Fatalf("queued edit attachment still bound to %q", *attachmentMessageID)
	}
}

func TestCancelTaskByUser_QueuedEditRollsBackOnRestoreFailure(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "QueuedEditRollbackAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID, messageID, attachmentID := insertQueuedChatInputWithAttachment(
		t,
		agentID,
		sessionID,
		"do not lose this queued prompt",
	)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
		VALUES ($1, $2, $3, 'collision', '{}'::uuid[])
	`, messageID, sessionID, taskID); err != nil {
		t.Fatalf("seed restore collision: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_draft_restore WHERE id = $1`, messageID)
	})

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(
		w,
		cancelQueuedTaskByUserRequest(t, testUserID, taskID, sessionID, "edit"),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "queued" {
		t.Fatalf("failed queued edit left task status %q", got)
	}

	var bindingCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM chat_message AS message
		JOIN attachment ON attachment.chat_message_id = message.id
		WHERE message.id = $1 AND attachment.id = $2
	`, messageID, attachmentID).Scan(&bindingCount); err != nil {
		t.Fatalf("read rolled back queued input: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("failed queued edit did not roll back input binding: count = %d", bindingCount)
	}
}

func TestCancelTaskByUser_QueuedRemoveDeletesAttachment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "QueuedRemoveAttachmentAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID, messageID, attachmentID := insertQueuedChatInputWithAttachment(
		t,
		agentID,
		sessionID,
		"discard this queued prompt",
	)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(
		w,
		cancelQueuedTaskByUserRequest(t, testUserID, taskID, sessionID, "remove"),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("queued remove task status = %q, want cancelled", got)
	}

	var rows int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM chat_message WHERE id = $1) +
			(SELECT count(*) FROM attachment WHERE id = $2)
	`, messageID, attachmentID).Scan(&rows); err != nil {
		t.Fatalf("count discarded queued input rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("queued remove left %d message/attachment rows", rows)
	}
}

// createStartedEmptyChatTask seeds the exact shape the deferred cancel is about:
// a chat task the daemon has already started (started_at set) whose transcript is
// still empty, plus the user message that triggered it.
func createStartedEmptyChatTask(t *testing.T, sessionID, agentID, content string) (taskID, userMessageID string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id, started_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2, now())
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create started chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, content, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}
	return taskID, userMessageID
}

func chatFinalizeDeferredAt(t *testing.T, taskID string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT chat_finalize_deferred_at FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&at); err != nil {
		t.Fatalf("read chat_finalize_deferred_at: %v", err)
	}
	return at
}

// Rollout compatibility (#5219). Clients and server do not upgrade together, and
// a started-but-empty cancel is the one case where the prompt does NOT come back
// in the cancel response — it is deferred and later published as a durable
// draft-restore row, which only a client that knows about chat:cancel_finalized
// and the draft-restores endpoint can collect. An installed desktop build that
// predates all of that would read the empty response as "nothing to restore" and
// silently drop the user's input, so deferral is gated on the client advertising
// AppCapabilityChatDraftRestoreV1.
func TestCancelTaskByUser_StartedEmptyChat_WithDraftRestoreCapability_Defers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatDeferCapableAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID, userMessageID := createStartedEmptyChatTask(t, sessionID, agentID, "defer this prompt")

	req := cancelTaskByUserRequest(t, testUserID, taskID)
	req.Header.Set("X-Client-Capabilities", protocol.AppCapabilityChatDraftRestoreV1)
	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage != nil {
		t.Fatalf("capable client must get no synchronous restore, got %#v", resp.CancelledChatMessage)
	}
	if chatFinalizeDeferredAt(t, taskID) == nil {
		t.Fatal("expected the finalize-deferred marker to be set")
	}

	// The judgment is deferred, so the user message must still be there for the
	// daemon ack / sweeper to settle.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID).Scan(&count); err != nil {
		t.Fatalf("count user chat message: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the user message to survive the deferral, got %d rows", count)
	}
}

func TestCancelTaskByUser_StartedEmptyChat_LegacyClient_StillGetsSynchronousRestore(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatDeferLegacyAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	const userContent = "an old client must not lose this"
	taskID, userMessageID := createStartedEmptyChatTask(t, sessionID, agentID, userContent)

	// No X-Client-Capabilities: a build that predates the durable restore.
	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("legacy client must still get the synchronous restore payload")
	}
	if resp.CancelledChatMessage.MessageID != userMessageID ||
		resp.CancelledChatMessage.Content != userContent ||
		!resp.CancelledChatMessage.RestoreToInput {
		t.Fatalf("restore payload mismatch: %#v", resp.CancelledChatMessage)
	}
	if at := chatFinalizeDeferredAt(t, taskID); at != nil {
		t.Fatalf("legacy cancel must not defer, marker set at %v", at)
	}

	// Legacy semantics all the way: the message is settled synchronously, and no
	// durable restore row is written for a client that could never read it.
	var messages int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID).Scan(&messages); err != nil {
		t.Fatalf("count user chat message: %v", err)
	}
	if messages != 0 {
		t.Fatalf("expected the user message to be deleted synchronously, got %d rows", messages)
	}
	var restores int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID).Scan(&restores); err != nil {
		t.Fatalf("count draft restores: %v", err)
	}
	if restores != 0 {
		t.Fatalf("expected no durable restore for a legacy cancel, got %d", restores)
	}
}

// TestCancelTaskByUser_RunOnlyAutopilot_Succeeds is the core MUL-2827 fix: a
// run_only autopilot task (issue_id + chat_session_id NULL, only
// autopilot_run_id set) is cancellable by a member of its agent's workspace.
func TestCancelTaskByUser_RunOnlyAutopilot_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelRunOnlyAgent", []byte("[]"))
	taskID := createAutopilotRunOnlyTask(t, agentID)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

// TestCancelTaskByUser_RunOnlyAutopilot_CrossWorkspace_Returns404 verifies the
// tenant guard: a member of workspace A cannot cancel a run_only task whose
// agent lives in workspace B, and the task is not mutated before the check.
func TestCancelTaskByUser_RunOnlyAutopilot_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignAgentID := createForeignWorkspaceAgent(t)
	taskID := createAutopilotRunOnlyTask(t, foreignAgentID)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "queued" {
		t.Fatalf("foreign task was mutated: status = %q", got)
	}
}

// TestCancelTaskByUser_QuickCreate_Succeeds verifies a quick_create task — no
// issue yet, no chat session, only context JSONB — is cancellable during its
// active window (the pre-issue-creation phase, i.e. whenever the user clicks X).
func TestCancelTaskByUser_QuickCreate_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelQuickCreateAgent", []byte("[]"))

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, context)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL,
		        '{"type":"quick_create","workspace_id":"ws","prompt":"do a thing"}'::jsonb)
		RETURNING id
	`, agentID).Scan(&taskID); err != nil {
		t.Fatalf("create quick_create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

// TestCancelTaskByUser_RetryClone_Autopilot_Succeeds verifies a retry clone of
// an autopilot task — which copies parent_task_id + autopilot_run_id verbatim,
// inheriting the NULL issue/chat links — is still cancellable.
func TestCancelTaskByUser_RetryClone_Autopilot_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelRetryCloneAgent", []byte("[]"))
	parentID := createAutopilotRunOnlyTask(t, agentID)

	var cloneID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, autopilot_run_id, parent_task_id, attempt)
		SELECT agent_id, runtime_id, 'queued', priority, autopilot_run_id, id, 1
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, parentID).Scan(&cloneID); err != nil {
		t.Fatalf("create retry clone: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, cloneID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, cloneID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, cloneID); got != "cancelled" {
		t.Fatalf("clone not cancelled: status = %q", got)
	}
}

// TestCancelTaskByUser_IssueTask_Succeeds is a regression guard: issue-bound
// tasks (the original supported case) stay cancellable after the rewrite.
func TestCancelTaskByUser_IssueTask_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelIssueTaskAgent", []byte("[]"))

	var issueID, taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cancel-byid-issue', 'todo', 'medium', $2, 'member', 92001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'queued', 0, $2)
		RETURNING id
	`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create issue task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

func TestCancelTaskByUser_DelegatedFailureRecoveryAcknowledgesSignal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "CancelRecoveryTaskAgent", []byte("[]"))
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cancel-recovery-task', 'in_progress', 'medium', $2, 'member', 92004, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	var sourceTaskID, failedTaskID, recoveryCommentID, recoveryTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, completed_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'completed', 0, now())
		RETURNING id
	`, agentID, issueID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create source task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, completed_at,
			delegated_from_task_id, trigger_evidence_kind
		)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'failed', 0, now(), $3, 'comment')
		RETURNING id
	`, agentID, issueID, sourceTaskID).Scan(&failedTaskID); err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, source_task_id)
		VALUES ($1, $2, 'system', $3, 'resume delegated coordination', 'progress_update', $4)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, failedTaskID).Scan(&recoveryCommentID); err != nil {
		t.Fatalf("create recovery comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
			trigger_evidence_kind, trigger_evidence_ref_id
		)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3,
		        'delegated_failure', $4)
		RETURNING id
	`, agentID, issueID, recoveryCommentID, failedTaskID).Scan(&recoveryTaskID); err != nil {
		t.Fatalf("create recovery task: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, recoveryTaskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var status string
	var acknowledged bool
	if err := testPool.QueryRow(ctx, `
		SELECT status, $2::uuid = ANY(delivered_comment_ids)
		FROM agent_task_queue WHERE id = $1
	`, recoveryTaskID, recoveryCommentID).Scan(&status, &acknowledged); err != nil {
		t.Fatalf("read cancelled recovery task: %v", err)
	}
	if status != "cancelled" || !acknowledged {
		t.Fatalf("cancelled recovery status/ack = %q/%v, want cancelled/true", status, acknowledged)
	}
}

// TestCancelTaskByUser_ChatTask_NonCreator_Returns403 preserves chat privacy:
// a workspace member who did not start the conversation cannot cancel its task.
func TestCancelTaskByUser_ChatTask_NonCreator_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID) // creator = testUserID
	otherUserID := createWorkspaceMemberUser(t, "Chat Bystander", "cancel-chat-bystander@multica.test")

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, otherUserID, taskID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "running" {
		t.Fatalf("chat task was mutated: status = %q", got)
	}
}

func TestCancelTaskByUser_ChatTaskWithTranscript_PersistsAssistantSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id, created_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2, now() - interval '5 seconds')
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', 'please answer', $2)
	`, sessionID, taskID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'text', 'partial answer')
	`, taskID); err != nil {
		t.Fatalf("create task message: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage != nil {
		t.Fatalf("expected no restore payload when transcript exists, got %#v", resp.CancelledChatMessage)
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}

	var role, content, messageTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role, content, COALESCE(task_id::text, '')
		FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant'
	`, sessionID).Scan(&role, &content, &messageTaskID); err != nil {
		t.Fatalf("read cancelled assistant chat message: %v", err)
	}
	if role != "assistant" || content != "Stopped." || messageTaskID != taskID {
		t.Fatalf("assistant snapshot mismatch: role=%q content=%q task_id=%q", role, content, messageTaskID)
	}
}

func TestCancelTaskByUser_ChatTaskWithoutTranscript_RestoresUserDraft(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatNoTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	var userMessageID string
	const userContent = "keep this prompt"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, userContent, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("expected restore payload for empty transcript cancel")
	}
	if resp.CancelledChatMessage.MessageID != userMessageID ||
		resp.CancelledChatMessage.Content != userContent ||
		!resp.CancelledChatMessage.RestoreToInput {
		t.Fatalf("restore payload mismatch: %#v", resp.CancelledChatMessage)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant'
	`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count assistant chat messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no assistant snapshot for empty transcript, got %d", count)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE id = $1
	`, userMessageID).Scan(&count); err != nil {
		t.Fatalf("count deleted user chat message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected linked user message to be deleted, got %d", count)
	}
}

func TestCancelTaskByUser_ChatRetryRestoresRootInput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatRetryAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	var rootTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, chat_session_id, completed_at
		)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'completed', 0, $2, now())
		RETURNING id
	`, agentID, sessionID).Scan(&rootTaskID); err != nil {
		t.Fatalf("create root chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, rootTaskID)
	})
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`,
		rootTaskID,
	); err != nil {
		t.Fatalf("seal root chat input: %v", err)
	}

	var messageID string
	const content = "restore the retry input"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, content, rootTaskID).Scan(&messageID); err != nil {
		t.Fatalf("create root chat message: %v", err)
	}

	var retryTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, chat_session_id,
			parent_task_id, retry_of_task_id, chat_input_task_id, attempt
		)
		VALUES (
			$1, (SELECT runtime_id FROM agent WHERE id = $1), 'queued', 0, $2,
			$3, $3, $3, 1
		)
		RETURNING id
	`, agentID, sessionID, rootTaskID).Scan(&retryTaskID); err != nil {
		t.Fatalf("create retry chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, retryTaskID)
	})

	transcript, err := testHandler.Queries.ListChatMessages(
		context.Background(),
		util.MustParseUUID(sessionID),
	)
	if err != nil {
		t.Fatalf("list transcript with queued retry: %v", err)
	}
	if len(transcript) != 1 || transcript[0].Content != content {
		t.Fatalf("queued retry hid its historical root input: %+v", transcript)
	}

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, retryTaskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode retry cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil ||
		resp.CancelledChatMessage.MessageID != messageID ||
		resp.CancelledChatMessage.Content != content {
		t.Fatalf("retry did not restore its root input: %#v", resp.CancelledChatMessage)
	}
}

// TestCancelTaskByUser_ChatTaskWithBoundAttachment_SurvivesCancelAndRebinds
// guards the data-loss path on the empty-chat cancel: the user message bound to
// an attachment is deleted, and attachment.chat_message_id is ON DELETE CASCADE
// (server/migrations/083_attachment_chat_columns.up.sql), so without the
// detach-before-delete step the cancel would silently destroy the user's
// attachment. The detach (chat_message_id -> NULL, chat_session_id retained) is
// load-bearing, not an optimization; nothing else covered it. This pins:
//
//	(a) the attachment row survives the cascade — still present, chat_message_id
//	    NULL, chat_session_id retained;
//	(b) the cancel response returns it via cancelled_chat_message.attachments so
//	    the restored draft can re-show it;
//	(c) re-sending the restored draft re-binds the surviving attachment to the
//	    new message in the same session.
func TestCancelTaskByUser_ChatTaskWithBoundAttachment_SurvivesCancelAndRebinds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatAttachAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	var userMessageID string
	const userContent = "look at this attachment"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, userContent, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}

	// Bind an attachment to that user message, exactly as a real send does:
	// workspace-scoped, uploaded by the session creator, pointing at both the
	// session and the message.
	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, chat_session_id, chat_message_id)
		VALUES ($1, 'member', $2, 'cancel-survive.png', 'https://cdn.example.com/cancel-survive.png', 'image/png', 9, $3, $4)
		RETURNING id::text
	`, testWorkspaceID, testUserID, sessionID, userMessageID).Scan(&attachmentID); err != nil {
		t.Fatalf("seed bound attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID) })

	// Cancel the empty chat task (no transcript) — this deletes the user message.
	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("expected restore payload for empty transcript cancel")
	}

	// (b) The cancel response carries the detached attachment back.
	var returned *AttachmentResponse
	for i := range resp.CancelledChatMessage.Attachments {
		if resp.CancelledChatMessage.Attachments[i].ID == attachmentID {
			returned = &resp.CancelledChatMessage.Attachments[i]
			break
		}
	}
	if returned == nil {
		t.Fatalf("cancel response did not return the detached attachment: %#v", resp.CancelledChatMessage.Attachments)
	}

	// (a) The row survived the ON DELETE CASCADE: still present, detached from
	//     the deleted message, but still scoped to the session.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&count); err != nil {
		t.Fatalf("count attachment: %v", err)
	}
	if count != 1 {
		t.Fatalf("attachment was cascade-deleted on cancel: count = %d", count)
	}
	var dbMessageID, dbSessionID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text, chat_session_id::text FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&dbMessageID, &dbSessionID); err != nil {
		t.Fatalf("read attachment after cancel: %v", err)
	}
	if dbMessageID != nil {
		t.Fatalf("expected chat_message_id detached to NULL, got %q", *dbMessageID)
	}
	if dbSessionID == nil || *dbSessionID != sessionID {
		t.Fatalf("expected chat_session_id retained as %q, got %v", sessionID, dbSessionID)
	}

	// Sanity: the empty-cancel still deleted the user message itself.
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID,
	).Scan(&count); err != nil {
		t.Fatalf("count user message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected linked user message to be deleted, got %d", count)
	}

	// (c) Re-sending the restored draft re-binds the surviving attachment to a
	//     fresh message in the same session — the whole reason for detaching.
	sendReq := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content":        userContent,
		"attachment_ids": []string{attachmentID},
	})
	sendReq = withURLParam(sendReq, "sessionId", sessionID)
	sendReq = withChatTestWorkspaceCtx(t, sendReq)
	sendW := httptest.NewRecorder()
	testHandler.SendChatMessage(sendW, sendReq)
	if sendW.Code != http.StatusCreated {
		t.Fatalf("resend: expected 201, got %d: %s", sendW.Code, sendW.Body.String())
	}
	var sendResp SendChatMessageResponse
	if err := json.Unmarshal(sendW.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("decode resend response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, sendResp.TaskID)
	})

	rebound := false
	for _, id := range sendResp.AttachmentIDs {
		if id == attachmentID {
			rebound = true
			break
		}
	}
	if !rebound {
		t.Fatalf("attachment not re-bound on resend: %#v", sendResp.AttachmentIDs)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&dbMessageID); err != nil {
		t.Fatalf("read attachment after resend: %v", err)
	}
	if dbMessageID == nil || *dbMessageID != sendResp.MessageID {
		t.Fatalf("expected attachment re-bound to new message %q, got %v", sendResp.MessageID, dbMessageID)
	}
}

// TestCancelTaskByUser_PrivateAgent_PlainMember_Returns403 verifies the cancel
// endpoint mirrors the agent Activity / snapshot visibility gate: a plain
// member who cannot see a private agent's tasks cannot cancel them either.
func TestCancelTaskByUser_PrivateAgent_PlainMember_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, _, memberID := privateAgentTestFixture(t)
	taskID := createAutopilotRunOnlyTask(t, agentID)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, memberID, taskID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "queued" {
		t.Fatalf("task was mutated: status = %q", got)
	}
}
