package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// chatPendingCtxAs injects the workspace + member context that the chi
// workspace middleware would normally set, resolved for an arbitrary user.
// The pending-tasks handlers read the workspace id from ctxWorkspaceID and the
// caller's role from the member context, so a direct handler call needs both.
func chatPendingCtxAs(t *testing.T, req *http.Request, userID string) *http.Request {
	t.Helper()
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(userID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member row for %s: %v", userID, err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

// insertChatSessionAs inserts a chat_session owned by an explicit creator +
// agent (createHandlerTestChatSession hardcodes testUserID as creator, which
// these permission tests need to vary).
func insertChatSessionAs(t *testing.T, agentID, creatorID string) string {
	t.Helper()
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'pending-tasks-test', 'active')
		RETURNING id
	`, testWorkspaceID, agentID, creatorID).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	return sessionID
}

// insertPendingChatTask seeds an in-flight agent_task_queue row bound to a chat
// session and returns its id.
func insertPendingChatTask(t *testing.T, agentID, sessionID, status string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, $3, 0, $4)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), status, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert pending chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func decodePendingTasks(t *testing.T, w *httptest.ResponseRecorder) PendingChatTasksResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp PendingChatTasksResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode pending tasks: %v", err)
	}
	return resp
}

func decodeHasPending(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp HasPendingChatTasksResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode has-any: %v", err)
	}
	return resp.HasPending
}

func containsPendingTask(tasks []PendingChatTaskItem, taskID string) bool {
	for _, it := range tasks {
		if it.TaskID == taskID {
			return true
		}
	}
	return false
}

func TestGetPendingChatTask_ReturnsActiveHeadAndFIFOQueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "PendingSessionQueueAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	activeID := insertPendingChatTask(t, agentID, sessionID, "running")
	nextID := insertPendingChatTask(t, agentID, sessionID, "queued")
	laterID := insertPendingChatTask(t, agentID, sessionID, "queued")

	for _, row := range []struct {
		id        string
		createdAt string
		content   string
	}{
		{activeID, "2026-07-29T01:00:00Z", "active prompt"},
		{nextID, "2026-07-29T01:00:01Z", "next prompt"},
		{laterID, "2026-07-29T01:00:02Z", "later prompt"},
	} {
		if _, err := testPool.Exec(context.Background(), `
			UPDATE agent_task_queue
			SET created_at = $2, chat_input_task_id = id
			WHERE id = $1
		`, row.id, row.createdAt); err != nil {
			t.Fatalf("stamp pending task %s: %v", row.id, err)
		}
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO chat_message (chat_session_id, role, content, task_id, created_at)
			VALUES ($1, 'user', $2, $3, $4)
		`, sessionID, row.content, row.id, row.createdAt); err != nil {
			t.Fatalf("insert pending message %s: %v", row.id, err)
		}
	}
	transcript, err := testHandler.Queries.ListChatMessages(
		context.Background(),
		util.MustParseUUID(sessionID),
	)
	if err != nil {
		t.Fatalf("list compatibility transcript: %v", err)
	}
	if len(transcript) != 1 || util.UUIDToString(transcript[0].TaskID) != activeID {
		t.Fatalf("queued prompts leaked into legacy transcript: %+v", transcript)
	}
	page, err := testHandler.Queries.ListChatMessagesPage(
		context.Background(),
		db.ListChatMessagesPageParams{
			ChatSessionID: util.MustParseUUID(sessionID),
			Limit:         50,
		},
	)
	if err != nil {
		t.Fatalf("list paged compatibility transcript: %v", err)
	}
	if len(page) != 1 || util.UUIDToString(page[0].TaskID) != activeID {
		t.Fatalf("queued prompts leaked into paged transcript: %+v", page)
	}

	req := withURLParam(
		newRequestAs(
			testUserID,
			http.MethodGet,
			"/api/chat/sessions/"+sessionID+"/pending-task",
			nil,
		),
		"sessionId",
		sessionID,
	)
	w := httptest.NewRecorder()
	testHandler.GetPendingChatTask(w, chatPendingCtxAs(t, req, testUserID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp PendingChatTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode pending task queue: %v", err)
	}
	if resp.TaskID != activeID || resp.Status != "running" {
		t.Fatalf("unexpected active head: %+v", resp)
	}
	if !resp.SupportsQueue {
		t.Fatal("pending response must advertise queue support")
	}
	if len(resp.QueuedTasks) != 2 {
		t.Fatalf("expected two queued tasks, got %+v", resp.QueuedTasks)
	}
	if resp.QueuedTasks[0].TaskID != nextID ||
		resp.QueuedTasks[0].Content != "next prompt" ||
		resp.QueuedTasks[1].TaskID != laterID ||
		resp.QueuedTasks[1].Content != "later prompt" {
		t.Fatalf("queue is not FIFO with message summaries: %+v", resp.QueuedTasks)
	}
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE agent_task_queue SET priority = 4 WHERE id = $1`,
		nextID,
	); err != nil {
		t.Fatalf("seed prior send-now selection: %v", err)
	}

	prioritizeReq := newRequestAs(
		testUserID,
		http.MethodPost,
		"/api/chat/sessions/"+sessionID+"/queued-tasks/"+laterID+"/prioritize",
		nil,
	)
	prioritizeReq = withURLParams(
		prioritizeReq,
		"sessionId", sessionID,
		"taskId", laterID,
	)
	prioritizeW := httptest.NewRecorder()
	testHandler.PrioritizeQueuedChatTask(
		prioritizeW,
		chatPendingCtxAs(t, prioritizeReq, testUserID),
	)
	if prioritizeW.Code != http.StatusOK {
		t.Fatalf("expected prioritize 200, got %d: %s", prioritizeW.Code, prioritizeW.Body.String())
	}
	var prioritized PrioritizeQueuedChatTaskResponse
	if err := json.Unmarshal(prioritizeW.Body.Bytes(), &prioritized); err != nil {
		t.Fatalf("decode prioritize response: %v", err)
	}
	if prioritized.TaskID != laterID || prioritized.ActiveTaskID != activeID {
		t.Fatalf("prioritize response is not server-authoritative: %+v", prioritized)
	}

	w = httptest.NewRecorder()
	testHandler.GetPendingChatTask(w, chatPendingCtxAs(t, req, testUserID))
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode prioritized queue: %v", err)
	}
	if resp.TaskID != activeID ||
		len(resp.QueuedTasks) != 2 ||
		resp.QueuedTasks[0].TaskID != laterID ||
		resp.QueuedTasks[1].TaskID != nextID {
		t.Fatalf("send-now priority did not move only the selected follow-up: %+v", resp)
	}

	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`,
		activeID,
	); err != nil {
		t.Fatalf("complete active task: %v", err)
	}
	w = httptest.NewRecorder()
	testHandler.GetPendingChatTask(w, chatPendingCtxAs(t, req, testUserID))
	resp = PendingChatTaskResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode all-queued state: %v", err)
	}
	if resp.TaskID != laterID ||
		resp.Status != "queued" ||
		len(resp.QueuedTasks) != 2 ||
		resp.QueuedTasks[0].TaskID != laterID ||
		resp.QueuedTasks[1].TaskID != nextID {
		t.Fatalf("all queued prompts must remain in the dedicated queue: %+v", resp)
	}
}

func TestClearQueuedChatTasks_CancelsWholeQueueAndDeletesInputs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ClearPendingQueueAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskIDs := []string{
		insertPendingChatTask(t, agentID, sessionID, "queued"),
		insertPendingChatTask(t, agentID, sessionID, "queued"),
	}
	for index, taskID := range taskIDs {
		if _, err := testPool.Exec(
			context.Background(),
			`UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`,
			taskID,
		); err != nil {
			t.Fatalf("stamp queued input owner %s: %v", taskID, err)
		}
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO chat_message (chat_session_id, role, content, task_id)
			VALUES ($1, 'user', $2, $3)
		`, sessionID, []string{"queued prompt A", "queued prompt B"}[index], taskID); err != nil {
			t.Fatalf("seed queued input %s: %v", taskID, err)
		}
	}

	req := withURLParam(
		newRequestAs(
			testUserID,
			http.MethodDelete,
			"/api/chat/sessions/"+sessionID+"/queued-tasks",
			nil,
		),
		"sessionId",
		sessionID,
	)
	w := httptest.NewRecorder()
	testHandler.ClearQueuedChatTasks(w, chatPendingCtxAs(t, req, testUserID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	for _, taskID := range taskIDs {
		if got := taskStatus(t, taskID); got != "cancelled" {
			t.Fatalf("queued task %s status = %q, want cancelled", taskID, got)
		}
	}
	var inputCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message WHERE task_id = $1 OR task_id = $2
	`, taskIDs[0], taskIDs[1]).Scan(&inputCount); err != nil {
		t.Fatalf("count queued inputs after clear: %v", err)
	}
	if inputCount != 0 {
		t.Fatalf("clear left %d queued input messages", inputCount)
	}
}

// TestListPendingChatTasks_HidesPrivateAgentFromLostAccessCreator verifies the
// P1 rewrite still enforces the private-agent gate now that filtering keys off
// the agent_id returned by the query (instead of a second session-list scan).
// A member who created a chat with a private agent they can no longer access
// must not see that task in the aggregate, while a task on a workspace-visible
// agent they still own is returned.
func TestListPendingChatTasks_HidesPrivateAgentFromLostAccessCreator(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingPublicAgent", []byte("[]"))

	// The plain member is the creator of BOTH sessions, so the creator_id
	// filter admits both; only the private-agent gate should drop one.
	privateSession := insertChatSessionAs(t, privateAgentID, memberID)
	publicSession := insertChatSessionAs(t, publicAgentID, memberID)
	privateTask := insertPendingChatTask(t, privateAgentID, privateSession, "running")
	publicTask := insertPendingChatTask(t, publicAgentID, publicSession, "queued")

	w := httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks", nil), memberID))
	resp := decodePendingTasks(t, w)

	if containsPendingTask(resp.Tasks, privateTask) {
		t.Fatalf("private-agent task %s leaked to plain member: %+v", privateTask, resp.Tasks)
	}
	if !containsPendingTask(resp.Tasks, publicTask) {
		t.Fatalf("public-agent task %s missing from plain member's pending list: %+v", publicTask, resp.Tasks)
	}
}

// TestListPendingChatTasks_OwnerSeesPrivateAgentTask is the positive control:
// the agent owner keeps visibility of their own private-agent chat task.
func TestListPendingChatTasks_OwnerSeesPrivateAgentTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, ownerID, _ := privateAgentTestFixture(t)
	session := insertChatSessionAs(t, privateAgentID, ownerID)
	task := insertPendingChatTask(t, privateAgentID, session, "running")

	w := httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(ownerID, "GET", "/api/chat/pending-tasks", nil), ownerID))
	resp := decodePendingTasks(t, w)

	if !containsPendingTask(resp.Tasks, task) {
		t.Fatalf("agent owner did not see their own private-agent task %s: %+v", task, resp.Tasks)
	}
}

// TestHasPendingChatTasks_FalseWhenOnlyInaccessiblePrivateAgent verifies the P3
// boolean endpoint preserves the same permission filtering as the list: a
// member whose only in-flight task is on a private agent they can't access
// gets has_pending=false (the FAB must not light up for a task they've lost
// access to).
func TestHasPendingChatTasks_FalseWhenOnlyInaccessiblePrivateAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	session := insertChatSessionAs(t, privateAgentID, memberID)
	insertPendingChatTask(t, privateAgentID, session, "running")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil), memberID))
	if decodeHasPending(t, w) {
		t.Fatalf("has-any returned true for a task on a private agent the member cannot access")
	}
}

// TestHasPendingChatTasks_TrueWhenAccessiblePublicAgent verifies the boolean
// endpoint returns true once the member has an in-flight task on an agent they
// can access — and that an inaccessible private-agent task alongside it does
// not change the (already true) answer.
func TestHasPendingChatTasks_TrueWhenAccessiblePublicAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingPublicAgentHasAny", []byte("[]"))

	privateSession := insertChatSessionAs(t, privateAgentID, memberID)
	publicSession := insertChatSessionAs(t, publicAgentID, memberID)
	insertPendingChatTask(t, privateAgentID, privateSession, "running")
	insertPendingChatTask(t, publicAgentID, publicSession, "waiting_local_directory")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil), memberID))
	if !decodeHasPending(t, w) {
		t.Fatalf("has-any returned false despite an in-flight task on an accessible public agent")
	}
}

// TestHasPendingChatTasks_OwnerOfPrivateAgentSeesTask confirms the boolean
// endpoint's positive path for a private agent's owner.
func TestHasPendingChatTasks_OwnerOfPrivateAgentSeesTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateAgentID, ownerID, _ := privateAgentTestFixture(t)
	session := insertChatSessionAs(t, privateAgentID, ownerID)
	insertPendingChatTask(t, privateAgentID, session, "dispatched")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(ownerID, "GET", "/api/chat/pending-tasks/has-any", nil), ownerID))
	if !decodeHasPending(t, w) {
		t.Fatalf("has-any returned false for the private agent's own owner")
	}
}

// TestHasPendingChatTasks_IgnoresTerminalTasks guards the status predicate: a
// completed task is not "pending", so the endpoint must return false.
func TestHasPendingChatTasks_IgnoresTerminalTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Use a freshly-created member (via the private-agent fixture) as the
	// creator so no stray in-flight task from another test can bleed into the
	// assertion — this user exists only for the duration of this test.
	_, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingTerminalAgent", []byte("[]"))
	session := insertChatSessionAs(t, publicAgentID, memberID)
	insertPendingChatTask(t, publicAgentID, session, "completed")

	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil), memberID))
	if decodeHasPending(t, w) {
		t.Fatalf("has-any returned true for a terminal (completed) task")
	}
}

// TestHasPendingChatTasks_HidesOtherCreatorsTask locks the cs.creator_id gate:
// user A's in-flight task on a workspace-visible agent — one B can freely
// access — must still return has_pending=false for B, because B is not the
// creator. This is the tenant boundary that the agent-visibility filter does
// NOT cover (both users can see the agent), so it needs its own guard.
func TestHasPendingChatTasks_HidesOtherCreatorsTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// ownerID = user A (task creator), memberID = user B (different member).
	// Both are plain workspace members who can access a workspace-visible agent.
	_, creatorA, otherB := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingCrossCreatorAgent", []byte("[]"))
	session := insertChatSessionAs(t, publicAgentID, creatorA)
	insertPendingChatTask(t, publicAgentID, session, "running")

	// B is not the creator → the creator_id filter must drop A's task.
	w := httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(otherB, "GET", "/api/chat/pending-tasks/has-any", nil), otherB))
	if decodeHasPending(t, w) {
		t.Fatalf("has-any leaked user A's task to user B (cs.creator_id gate not enforced)")
	}

	// Sanity: A (the creator) does see their own task on the same agent.
	w = httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(creatorA, "GET", "/api/chat/pending-tasks/has-any", nil), creatorA))
	if !decodeHasPending(t, w) {
		t.Fatalf("has-any returned false for the task's own creator")
	}

	// The detailed list endpoint enforces the same creator gate.
	w = httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(t, newRequestAs(otherB, "GET", "/api/chat/pending-tasks", nil), otherB))
	if resp := decodePendingTasks(t, w); len(resp.Tasks) != 0 {
		t.Fatalf("list leaked user A's task to user B: %+v", resp.Tasks)
	}
}
