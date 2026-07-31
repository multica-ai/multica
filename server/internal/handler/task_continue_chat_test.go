package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ContinueTaskInChat (POST /api/tasks/{taskId}/continue-in-chat) opens a chat
// that resumes a finished background run. The risk in this endpoint is not the
// insert — it is what it promises: a chat that claims to continue a run but
// silently lost the resume pointer is worse than no button, because the member
// cannot tell the difference until the agent answers as if it had never seen the
// work. These tests pin (a) which pointer is inherited and from where, (b) the
// cases where the pointer is deliberately withheld and the response says so, and
// (c) that a second click reopens rather than forks.

// ─── Decision logic (no database required) ─────────────────────────────────

func textValue(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

// TestResumePointerFromTask_CarriesBothWhenHealthy is the happy path: a cleanly
// completed task hands over both halves of the pointer.
func TestResumePointerFromTask_CarriesBothWhenHealthy(t *testing.T) {
	session, workDir := resumePointerFromTask(db.AgentTaskQueue{
		Status:    "completed",
		SessionID: textValue("sess-abc"),
		WorkDir:   textValue("/work/abc"),
	})
	if !session.Valid || session.String != "sess-abc" {
		t.Errorf("session = %+v, want sess-abc", session)
	}
	if !workDir.Valid || workDir.String != "/work/abc" {
		t.Errorf("work_dir = %+v, want /work/abc", workDir)
	}
}

// TestResumePointerFromTask_WithholdsSessionKeepsWorkDir covers the cases where
// resuming the conversation cannot work but reusing the directory still can. The
// two halves must be independent: collapsing them would either resume poison or
// throw away a perfectly good workdir.
func TestResumePointerFromTask_WithholdsSessionKeepsWorkDir(t *testing.T) {
	cases := []struct {
		name string
		task db.AgentTaskQueue
	}{
		{
			// A failure whose conversation cannot survive a resume. Same
			// judgment the rerun path applies to its source task.
			name: "resume-unsafe failure reason",
			task: db.AgentTaskQueue{
				Status:        "failed",
				SessionID:     textValue("sess-poisoned"),
				WorkDir:       textValue("/work/poisoned"),
				FailureReason: textValue("agent_error"),
				Error:         textValue("API error 400 invalid_request_error: messages must not be empty"),
			},
		},
		{
			// MUL-5305: session_id is set but its rollout never landed, so
			// there is nothing on disk to resume from.
			name: "session rollout missing",
			task: db.AgentTaskQueue{
				Status:                "completed",
				SessionID:             textValue("sess-no-rollout"),
				WorkDir:               textValue("/work/no-rollout"),
				SessionRolloutMissing: true,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session, workDir := resumePointerFromTask(tc.task)
			if session.Valid {
				t.Errorf("session should be withheld, got %q", session.String)
			}
			if !workDir.Valid {
				t.Errorf("work_dir should still be offered, got %+v", workDir)
			}
		})
	}
}

// TestResumePointerFromTask_NoSessionRecorded is the kiro/kimi/qoder/traecli
// shape: those backends emit no session status at all, so a task that never
// reached its terminal report has a work_dir and no session. The continuation
// must still open (in that directory) rather than fail.
func TestResumePointerFromTask_NoSessionRecorded(t *testing.T) {
	session, workDir := resumePointerFromTask(db.AgentTaskQueue{
		Status:  "cancelled",
		WorkDir: textValue("/work/only"),
	})
	if session.Valid {
		t.Errorf("session = %+v, want withheld", session)
	}
	if !workDir.Valid || workDir.String != "/work/only" {
		t.Errorf("work_dir = %+v, want /work/only", workDir)
	}
}

// TestResumePointerFromTask_BlankStringsAreAbsent guards against a whitespace-only
// column reading as a real pointer — that would hand the daemon an unusable
// session id and report session_carried=true to the user.
func TestResumePointerFromTask_BlankStringsAreAbsent(t *testing.T) {
	session, workDir := resumePointerFromTask(db.AgentTaskQueue{
		Status:    "completed",
		SessionID: textValue("   "),
		WorkDir:   textValue(""),
	})
	if session.Valid {
		t.Errorf("blank session should be absent, got %q", session.String)
	}
	if workDir.Valid {
		t.Errorf("blank work_dir should be absent, got %q", workDir.String)
	}
}

// TestIsTerminalTaskStatus enumerates the whole status domain from the
// agent_task_queue CHECK constraint so a newly added status fails loudly here
// instead of silently becoming "continuable" while it still owns its session and
// workdir.
func TestIsTerminalTaskStatus(t *testing.T) {
	terminal := map[string]bool{
		"completed": true, "failed": true, "cancelled": true,
		"queued": false, "dispatched": false, "running": false,
		"waiting_local_directory": false, "deferred": false,
	}
	for status, want := range terminal {
		if got := isTerminalTaskStatus(status); got != want {
			t.Errorf("isTerminalTaskStatus(%q) = %v, want %v", status, got, want)
		}
	}
}

// TestTruncateChatTitle_IsRuneSafe pins multibyte correctness: byte slicing a CJK
// issue title would split a character and emit a replacement glyph in the chat
// list.
func TestTruncateChatTitle_IsRuneSafe(t *testing.T) {
	short := "Fix the login redirect"
	if got := truncateChatTitle(short); got != short {
		t.Errorf("short title was altered: %q", got)
	}

	long := strings.Repeat("修", continueTaskInChatTitleLimit+20)
	got := truncateChatTitle(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long title should be ellipsized, got %q", got)
	}
	if !isValidUTF8(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
	if runes := len([]rune(strings.TrimSuffix(got, "…"))); runes > continueTaskInChatTitleLimit {
		t.Errorf("kept %d runes, want <= %d", runes, continueTaskInChatTitleLimit)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

// ─── Handler behaviour (database required) ─────────────────────────────────

func continueInChatRequest(t *testing.T, userID, taskID string) *http.Request {
	t.Helper()
	req := newRequestAs(userID, "POST", "/api/tasks/"+taskID+"/continue-in-chat", nil)
	req = withURLParam(req, "taskId", taskID)
	return withChatTestWorkspaceCtx(t, req)
}

// createTerminalIssueTask seeds the row the button acts on: a finished issue task
// carrying a resume pointer. runtimeID is explicit so a test can prove the
// continuation follows the TASK's runtime rather than the agent's current one.
func createTerminalIssueTask(t *testing.T, agentID, issueID, runtimeID, status, sessionID, workDir string) string {
	t.Helper()
	var sess, wd any
	if sessionID != "" {
		sess = sessionID
	}
	if workDir != "" {
		wd = workDir
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, session_id, work_dir, completed_at)
		VALUES ($1, $2, $3, 0, $4, $5, $6, now())
		RETURNING id
	`, agentID, runtimeID, status, issueID, sess, wd).Scan(&taskID); err != nil {
		t.Fatalf("create terminal issue task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

func readChatSessionRow(t *testing.T, id string) db.ChatSession {
	t.Helper()
	row, err := testHandler.Queries.GetChatSession(context.Background(), mustUUID(t, id))
	if err != nil {
		t.Fatalf("read chat session %s: %v", id, err)
	}
	return row
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

func decodeContinueResponse(t *testing.T, w *httptest.ResponseRecorder) ContinueTaskInChatResponse {
	t.Helper()
	var resp ContinueTaskInChatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, resp.ChatSession.ID)
	})
	return resp
}

// TestContinueTaskInChat_CarriesResumePointerFromTask is the core contract: the
// created chat_session holds the source task's session id, work_dir AND runtime,
// because those three are exactly what the daemon's chat claim reads to resume.
//
// The runtime assertion is the one that matters most and is easiest to get wrong:
// CreateChatSession derives runtime_id from the AGENT, and reusing it here would
// silently discard the session whenever the agent has since been re-bound. This
// test therefore pins the task's runtime to a second runtime that is NOT the
// agent's, and asserts the continuation follows the task.
func TestContinueTaskInChat_CarriesResumePointerFromTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatAgent", []byte("[]"))
	issueID := createTestIssue(t, "Continue in chat happy path", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	otherRuntimeID := createSecondTestRuntime(t, "Continue Chat Other Runtime")
	taskID := createTerminalIssueTask(t, agentID, issueID, otherRuntimeID, "completed", "sess-carried", "/work/carried")

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeContinueResponse(t, w)

	if !resp.SessionCarried {
		t.Errorf("session_carried = false, want true")
	}
	if !resp.WorkDirCarried {
		t.Errorf("work_dir_carried = false, want true")
	}
	if resp.Reopened {
		t.Errorf("reopened = true on first call")
	}
	if resp.ChatSession.ContinuedFromTaskID == nil || *resp.ChatSession.ContinuedFromTaskID != taskID {
		t.Errorf("continued_from_task_id = %v, want %s", resp.ChatSession.ContinuedFromTaskID, taskID)
	}

	row := readChatSessionRow(t, resp.ChatSession.ID)
	if row.SessionID.String != "sess-carried" {
		t.Errorf("chat_session.session_id = %q, want sess-carried", row.SessionID.String)
	}
	if row.WorkDir.String != "/work/carried" {
		t.Errorf("chat_session.work_dir = %q, want /work/carried", row.WorkDir.String)
	}
	if got := uuidToString(row.RuntimeID); got != otherRuntimeID {
		t.Errorf("chat_session.runtime_id = %s, want the SOURCE TASK's runtime %s "+
			"(inheriting the agent's current runtime would silently drop the resume)", got, otherRuntimeID)
	}
	if row.AgentID.Valid && uuidToString(row.AgentID) != agentID {
		t.Errorf("chat_session.agent_id = %s, want %s", uuidToString(row.AgentID), agentID)
	}
}

// createSecondTestRuntime adds another runtime to the test workspace so a task
// can be pinned to a runtime that is not its agent's.
func createSecondTestRuntime(t *testing.T, name string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, $2, 'cloud', 'continue_chat_runtime', 'online', 'test runtime', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, name).Scan(&runtimeID); err != nil {
		t.Fatalf("create second runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID) })
	return runtimeID
}

// TestContinueTaskInChat_SecondCallReopens pins idempotency: clicking the button
// twice must reopen the same conversation, not fork a second one onto the same
// provider session (two chats resuming one session is the concurrency hazard this
// endpoint exists to avoid).
func TestContinueTaskInChat_SecondCallReopens(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatIdemAgent", []byte("[]"))
	issueID := createTestIssue(t, "Continue in chat idempotency", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	taskID := createTerminalIssueTask(t, agentID, issueID, handlerTestRuntimeID(t), "completed", "sess-idem", "/work/idem")

	first := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(first, continueInChatRequest(t, testUserID, taskID))
	if first.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	firstResp := decodeContinueResponse(t, first)

	second := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(second, continueInChatRequest(t, testUserID, taskID))
	if second.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", second.Code, second.Body.String())
	}
	var secondResp ContinueTaskInChatResponse
	if err := json.NewDecoder(second.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if !secondResp.Reopened {
		t.Errorf("reopened = false on second call, want true")
	}
	if secondResp.ChatSession.ID != firstResp.ChatSession.ID {
		t.Errorf("second call created a new session %s, want the existing %s",
			secondResp.ChatSession.ID, firstResp.ChatSession.ID)
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_session WHERE continued_from_task_id = $1`, taskID,
	).Scan(&count); err != nil {
		t.Fatalf("count continuations: %v", err)
	}
	if count != 1 {
		t.Errorf("continuation rows = %d, want exactly 1", count)
	}
}

// TestContinueTaskInChat_NonTerminalTaskRejected is the safety gate. A running
// task still owns its provider session and its work_dir, and a reused work_dir
// has no mutual exclusion, so continuing it would put two runs in one directory.
// This must refuse with a machine-readable reason and create nothing.
func TestContinueTaskInChat_NonTerminalTaskRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatRunningAgent", []byte("[]"))
	issueID := createTestIssue(t, "Continue in chat running", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory"} {
		t.Run(status, func(t *testing.T) {
			taskID := createTerminalIssueTask(t, agentID, issueID, handlerTestRuntimeID(t), status, "sess-live", "/work/live")

			w := httptest.NewRecorder()
			testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
			if w.Code != http.StatusConflict {
				t.Fatalf("expected 409 for %s, got %d: %s", status, w.Code, w.Body.String())
			}
			var body map[string]any
			json.NewDecoder(w.Body).Decode(&body)
			if body["reason"] != "task_not_terminal" {
				t.Errorf("reason = %v, want task_not_terminal", body["reason"])
			}

			var count int
			testPool.QueryRow(context.Background(),
				`SELECT count(*) FROM chat_session WHERE continued_from_task_id = $1`, taskID).Scan(&count)
			if count != 0 {
				t.Errorf("created %d chat sessions for a %s task, want 0", count, status)
			}
		})
	}
}

// TestContinueTaskInChat_ResumeUnsafeFailureOpensWithoutSession pins the honesty
// requirement: a task that died on a conversation a resume cannot survive still
// yields a usable chat (same directory), but the response must report
// session_carried=false so the UI does not claim continuity it does not have.
func TestContinueTaskInChat_ResumeUnsafeFailureOpensWithoutSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatPoisonAgent", []byte("[]"))
	issueID := createTestIssue(t, "Continue in chat poisoned session", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	taskID := createTerminalIssueTask(t, agentID, issueID, handlerTestRuntimeID(t), "failed", "sess-poison", "/work/poison")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue SET failure_reason = 'agent_error',
		       error = 'API error 400 invalid_request_error: messages must not be empty'
		 WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("mark task resume-unsafe: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeContinueResponse(t, w)

	if resp.SessionCarried {
		t.Errorf("session_carried = true for a resume-unsafe failure, want false")
	}
	if !resp.WorkDirCarried {
		t.Errorf("work_dir_carried = false, want true (the directory is still reusable)")
	}
	if row := readChatSessionRow(t, resp.ChatSession.ID); row.SessionID.Valid {
		t.Errorf("chat_session.session_id = %q, want NULL", row.SessionID.String)
	}
}

// TestContinueTaskInChat_ChatTaskRejected: a chat task already has its
// conversation. Minting a second session onto the same provider session is the
// one thing this endpoint must never do.
func TestContinueTaskInChat_ChatTaskRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatAlreadyChatAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID, _ := createStartedEmptyChatTask(t, sessionID, agentID, "hello")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID,
	); err != nil {
		t.Fatalf("finish chat task: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["reason"] != "already_chat_task" {
		t.Errorf("reason = %v, want already_chat_task", body["reason"])
	}
	if body["chat_session_id"] != sessionID {
		t.Errorf("chat_session_id = %v, want %s so the caller can navigate", body["chat_session_id"], sessionID)
	}
}

// TestContinueTaskInChat_CrossWorkspaceReturns404 pins tenancy: a task in another
// workspace must 404, not 403 — a 403 would confirm the id exists.
func TestContinueTaskInChat_CrossWorkspaceReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignAgentID := createForeignWorkspaceAgent(t)
	taskID := createAutopilotRunOnlyTask(t, foreignAgentID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID,
	); err != nil {
		t.Fatalf("finish foreign task: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a cross-workspace task, got %d: %s", w.Code, w.Body.String())
	}
}

// TestContinueTaskInChat_PrivateAgentBlocksPlainMember pins the admission gate.
// Continuing in chat starts agent runs, so it needs the INVOKE permission, not
// the softer visibility gate that cancel uses: a member who may watch (or stop) a
// private agent's run must not thereby be able to start new ones. The reason code
// is deliberately non-enumerating.
func TestContinueTaskInChat_PrivateAgentBlocksPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerID := createWorkspaceMemberUser(t, "Continue Chat Owner", "continue-chat-owner@multica.ai")
	outsiderID := createWorkspaceMemberUser(t, "Continue Chat Outsider", "continue-chat-outsider@multica.ai")
	agentID := createPrivateAgentOwnedBy(t, "ContinueChatPrivateAgent", ownerID)
	issueID := createTestIssue(t, "Continue in chat private agent", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	taskID := createTerminalIssueTask(t, agentID, issueID, handlerTestRuntimeID(t), "completed", "sess-private", "/work/private")

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, outsiderID, taskID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-owner member, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["reason_code"] != string(ReasonInvocationNotAllowed) {
		t.Errorf("reason_code = %v, want %s", body["reason_code"], ReasonInvocationNotAllowed)
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_session WHERE continued_from_task_id = $1`, taskID).Scan(&count)
	if count != 0 {
		t.Errorf("created %d chat sessions despite the block, want 0", count)
	}
}

// TestContinueTaskInChat_ArchivedAgentRejected: an archived agent cannot run, so
// a conversation with it would be a dead end.
func TestContinueTaskInChat_ArchivedAgentRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatArchivedAgent", []byte("[]"))
	issueID := createTestIssue(t, "Continue in chat archived agent", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	taskID := createTerminalIssueTask(t, agentID, issueID, handlerTestRuntimeID(t), "completed", "sess-archived", "/work/archived")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET archived_at = now() WHERE id = $1`, agentID,
	); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an archived agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestContinueTaskInChat_SeedsTitleFromIssue: the chat list is the surface a
// member scans, so a continuation has to be identifiable there without waiting
// for the auto-titler.
func TestContinueTaskInChat_SeedsTitleFromIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ContinueChatTitleAgent", []byte("[]"))
	const issueTitle = "Continue in chat title seeding"
	issueID := createTestIssue(t, issueTitle, "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	taskID := createTerminalIssueTask(t, agentID, issueID, handlerTestRuntimeID(t), "completed", "sess-title", "/work/title")

	w := httptest.NewRecorder()
	testHandler.ContinueTaskInChat(w, continueInChatRequest(t, testUserID, taskID))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeContinueResponse(t, w)
	if resp.ChatSession.Title != issueTitle {
		t.Errorf("title = %q, want %q", resp.ChatSession.Title, issueTitle)
	}
}
