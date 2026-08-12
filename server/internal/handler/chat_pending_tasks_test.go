package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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

func insertQueuedChatInputWithAttachment(
	t *testing.T,
	agentID, sessionID, content string,
) (taskID, messageID, attachmentID string) {
	t.Helper()
	taskID = insertPendingChatTask(t, agentID, sessionID, "queued")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("stamp queued input owner: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, content, taskID).Scan(&messageID); err != nil {
		t.Fatalf("insert queued input: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_draft_restore WHERE id = $1`, messageID)
	})
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (
			workspace_id, uploader_type, uploader_id, filename, url,
			content_type, size_bytes, chat_session_id, chat_message_id
		)
		VALUES (
			$1, 'member', $2, 'queued.png', 'https://cdn.example.com/queued.png',
			'image/png', 9, $3, $4
		)
		RETURNING id
	`, testWorkspaceID, testUserID, sessionID, messageID).Scan(&attachmentID); err != nil {
		t.Fatalf("insert queued attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})
	return taskID, messageID, attachmentID
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
	regenerateID := insertPendingChatTask(t, agentID, sessionID, "queued")
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE agent_task_queue SET regenerate_quick_actions_for = $2 WHERE id = $1`,
		regenerateID,
		uuid.New(),
	); err != nil {
		t.Fatalf("mark quick-actions regeneration task: %v", err)
	}

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
		t.Fatalf("legacy full-list transcript exposed queued prompts: %+v", transcript)
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
		t.Fatalf("paged transcript exposed queued prompts: %+v", page)
	}

	legacy, err := testHandler.Queries.ListChatMessagesForLegacyTask(
		context.Background(),
		util.MustParseUUID(sessionID),
	)
	if err != nil {
		t.Fatalf("list legacy task input: %v", err)
	}
	if len(legacy) != 1 || util.UUIDToString(legacy[0].TaskID) != activeID {
		t.Fatalf("legacy task input leaked queued successor: %+v", legacy)
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
		len(resp.QueuedTasks) != 1 ||
		resp.QueuedTasks[0].TaskID != nextID {
		t.Fatalf("the first queued row must become the head, not a follow-up: %+v", resp)
	}

	transcript, err = testHandler.Queries.ListChatMessages(
		context.Background(),
		util.MustParseUUID(sessionID),
	)
	if err != nil {
		t.Fatalf("list transcript after head promotion: %v", err)
	}
	if len(transcript) != 2 ||
		transcript[0].Content != "active prompt" ||
		transcript[1].Content != "later prompt" {
		t.Fatalf("transcript must expose the queued-status head only: %+v", transcript)
	}
}

func TestListChatMessagesPage_QueuedFollowUpsDoNotConsumeLimit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "QueuedPageBudgetAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	settledID := insertPendingChatTask(t, agentID, sessionID, "completed")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id, created_at)
		VALUES
			($1, 'user', 'settled 1', $2, '2026-07-29T01:00:00Z'),
			($1, 'assistant', 'settled 2', $2, '2026-07-29T01:00:01Z'),
			($1, 'user', 'settled 3', $2, '2026-07-29T01:00:02Z'),
			($1, 'assistant', 'settled 4', $2, '2026-07-29T01:00:03Z')
	`, sessionID, settledID); err != nil {
		t.Fatalf("insert settled history: %v", err)
	}

	for _, row := range []struct {
		content   string
		createdAt string
	}{
		{"queued prompt A", "2026-07-29T01:00:04Z"},
		{"queued prompt B", "2026-07-29T01:00:05Z"},
		{"queued prompt C", "2026-07-29T01:00:06Z"},
	} {
		taskID := insertPendingChatTask(t, agentID, sessionID, "queued")
		if _, err := testPool.Exec(ctx, `
			WITH owned AS (
				UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1
				RETURNING id
			)
			INSERT INTO chat_message (chat_session_id, role, content, task_id, created_at)
			SELECT $2, 'user', $3, id, $4 FROM owned
		`, taskID, sessionID, row.content, row.createdAt); err != nil {
			t.Fatalf("insert queued history: %v", err)
		}
	}

	t.Log("PAGING FIXTURE: endpoint=GET /api/chat/sessions/{id}/messages/page settled=4 head=1 follow_ups=2 limit=3")
	t.Log("REQUEST page=1: limit=3 before=<none>")
	page1 := fetchChatMessagesPageForTest(t, sessionID, url.Values{"limit": {"3"}})
	if len(page1.Messages) != 3 ||
		page1.Messages[0].Content != "settled 3" ||
		page1.Messages[1].Content != "settled 4" ||
		page1.Messages[2].Content != "queued prompt A" ||
		!page1.HasMore || page1.NextCursor == nil {
		t.Fatalf("unexpected first page: %+v", page1)
	}
	t.Logf("RESPONSE page=1: messages=[%q, %q, %q] has_more=%t", page1.Messages[0].Content, page1.Messages[1].Content, page1.Messages[2].Content, page1.HasMore)
	t.Logf("CURSOR page=1: created_at=%s id=%s", page1.NextCursor.CreatedAt, page1.NextCursor.ID)

	page2Params := url.Values{
		"limit":             {"3"},
		"before_created_at": {page1.NextCursor.CreatedAt},
		"before_id":         {page1.NextCursor.ID},
	}
	t.Logf("REQUEST page=2: limit=3 before_created_at=%s before_id=%s", page1.NextCursor.CreatedAt, page1.NextCursor.ID)
	page2 := fetchChatMessagesPageForTest(t, sessionID, page2Params)
	if len(page2.Messages) != 2 || page2.Messages[0].Content != "settled 1" || page2.Messages[1].Content != "settled 2" || page2.HasMore || page2.NextCursor != nil {
		t.Fatalf("unexpected second page: %+v", page2)
	}
	t.Logf("RESPONSE page=2: messages=[%q, %q] has_more=%t next_cursor=<nil>", page2.Messages[0].Content, page2.Messages[1].Content, page2.HasMore)
	t.Log("ASSERTION: page_sizes=3,2 head_visible=1 follow_ups_visible=0 cursor_advanced=true duplicates=0")
}

func TestPendingQueueHeadMatchesClaimForEqualCreatedAt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Pending Equal Timestamp Agent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	lowID := uuid.New()
	highID := lowID
	lowID[15] = 0
	highID[15] = 255
	for _, taskID := range []uuid.UUID{highID, lowID} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (
				id, agent_id, runtime_id, status, priority, chat_session_id, created_at
			)
			VALUES ($1, $2, $3, 'queued', 2, $4, '2026-07-29T01:00:00Z')
		`, taskID, agentID, handlerTestRuntimeID(t), sessionID); err != nil {
			t.Fatalf("insert equal-timestamp task %s: %v", taskID, err)
		}
	}

	pending, err := testHandler.Queries.ListPendingChatTasksForSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list pending tasks: %v", err)
	}
	if len(pending) != 2 || uuidToString(pending[0].ID) != lowID.String() {
		t.Fatalf("pending head = %+v, want lower id %s", pending, lowID)
	}

	claimed, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("claim returned no task")
	}
	if got := uuidToString(claimed.ID); got != lowID.String() {
		t.Fatalf("claimed task = %s, pending head = %s", got, lowID)
	}
}

func TestPrioritizeQueuedChatTask_StaleTargetPreservesExistingPriority(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "StalePrioritizeQueueAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	existingID := insertPendingChatTask(t, agentID, sessionID, "queued")
	staleID := insertPendingChatTask(t, agentID, sessionID, "completed")
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE agent_task_queue SET priority = 4 WHERE id = $1`,
		existingID,
	); err != nil {
		t.Fatalf("seed existing priority: %v", err)
	}

	req := withURLParams(
		newRequestAs(
			testUserID,
			http.MethodPost,
			"/api/chat/sessions/"+sessionID+"/queued-tasks/"+staleID+"/prioritize",
			nil,
		),
		"sessionId", sessionID,
		"taskId", staleID,
	)
	w := httptest.NewRecorder()
	testHandler.PrioritizeQueuedChatTask(w, chatPendingCtxAs(t, req, testUserID))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflictBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conflictBody); err != nil {
		t.Fatalf("decode stale-task conflict: %v", err)
	}
	if conflictBody.Error != "task is no longer queued" {
		t.Fatalf("stale-task error = %q", conflictBody.Error)
	}

	var priority int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT priority FROM agent_task_queue WHERE id = $1`,
		existingID,
	).Scan(&priority); err != nil {
		t.Fatalf("read preserved priority: %v", err)
	}
	if priority != 4 {
		t.Fatalf("stale prioritize demoted existing queue head to %d", priority)
	}
}

func TestPrioritizeQueuedChatTask_RejectsWhileVisibleHeadIsUnclaimed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "UnclaimedPrioritizeQueueAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	headID := insertPendingChatTask(t, agentID, sessionID, "queued")
	targetID := insertPendingChatTask(t, agentID, sessionID, "queued")
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET created_at = CASE id WHEN $1 THEN '2026-07-29T01:00:00Z'::timestamptz ELSE '2026-07-29T01:00:01Z'::timestamptz END
		WHERE id IN ($1, $2)
	`, headID, targetID); err != nil {
		t.Fatalf("order unclaimed queue: %v", err)
	}

	req := withURLParams(
		newRequestAs(
			testUserID,
			http.MethodPost,
			"/api/chat/sessions/"+sessionID+"/queued-tasks/"+targetID+"/prioritize",
			nil,
		),
		"sessionId", sessionID,
		"taskId", targetID,
	)
	w := httptest.NewRecorder()
	testHandler.PrioritizeQueuedChatTask(w, chatPendingCtxAs(t, req, testUserID))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflictBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conflictBody); err != nil {
		t.Fatalf("decode unclaimed-head conflict: %v", err)
	}
	if conflictBody.Error != "there is no active reply to replace" {
		t.Fatalf("unclaimed-head error = %q", conflictBody.Error)
	}

	pending, err := testHandler.Queries.ListPendingChatTasksForSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list preserved queue: %v", err)
	}
	if len(pending) != 2 || uuidToString(pending[0].ID) != headID || uuidToString(pending[1].ID) != targetID {
		t.Fatalf("rejected prioritize changed queue order: %+v", pending)
	}
}

func TestPrioritizeQueuedChatTask_BroadcastsQueueInvalidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "PrioritizeQueueBroadcastAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	insertPendingChatTask(t, agentID, sessionID, "running")
	taskID := insertPendingChatTask(t, agentID, sessionID, "queued")
	got := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventTaskQueued, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if !ok || payload["task_id"] != taskID {
			return
		}
		got <- event
	})

	req := withURLParams(
		newRequestAs(
			testUserID,
			http.MethodPost,
			"/api/chat/sessions/"+sessionID+"/queued-tasks/"+taskID+"/prioritize",
			nil,
		),
		"sessionId", sessionID,
		"taskId", taskID,
	)
	w := httptest.NewRecorder()
	testHandler.PrioritizeQueuedChatTask(w, chatPendingCtxAs(t, req, testUserID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case event := <-got:
		if event.WorkspaceID != testWorkspaceID {
			t.Fatalf("event workspace = %s, want %s", event.WorkspaceID, testWorkspaceID)
		}
	default:
		t.Fatal("prioritize did not broadcast task:queued invalidation")
	}
}

func TestClearQueuedChatTasks_PreservesUnclaimedHeadAndDeletesFollowUps(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ClearPendingQueueAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	var taskIDs, messageIDs, attachmentIDs []string
	for _, content := range []string{"queued prompt A", "queued prompt B"} {
		taskID, messageID, attachmentID := insertQueuedChatInputWithAttachment(
			t,
			agentID,
			sessionID,
			content,
		)
		taskIDs = append(taskIDs, taskID)
		messageIDs = append(messageIDs, messageID)
		attachmentIDs = append(attachmentIDs, attachmentID)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET created_at = CASE id WHEN $1 THEN '2026-07-29T01:00:00Z'::timestamptz ELSE '2026-07-29T01:00:01Z'::timestamptz END
		WHERE id IN ($1, $2)
	`, taskIDs[0], taskIDs[1]); err != nil {
		t.Fatalf("order queued tasks: %v", err)
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

	if got := taskStatus(t, taskIDs[0]); got != "queued" {
		t.Fatalf("unclaimed head status = %q, want queued", got)
	}
	if got := taskStatus(t, taskIDs[1]); got != "cancelled" {
		t.Fatalf("queued follow-up status = %q, want cancelled", got)
	}
	var headInputExists, followUpInputExists bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			EXISTS (SELECT 1 FROM chat_message WHERE id = $1),
			EXISTS (SELECT 1 FROM chat_message WHERE id = $2)
	`, messageIDs[0], messageIDs[1]).Scan(&headInputExists, &followUpInputExists); err != nil {
		t.Fatalf("read queued inputs after clear: %v", err)
	}
	if !headInputExists || followUpInputExists {
		t.Fatalf("queued inputs after clear: head=%t follow_up=%t, want true/false", headInputExists, followUpInputExists)
	}
	var headAttachmentExists, followUpAttachmentExists bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			EXISTS (SELECT 1 FROM attachment WHERE id = $1),
			EXISTS (SELECT 1 FROM attachment WHERE id = $2)
	`, attachmentIDs[0], attachmentIDs[1]).Scan(&headAttachmentExists, &followUpAttachmentExists); err != nil {
		t.Fatalf("read queued attachments after clear: %v", err)
	}
	if !headAttachmentExists || followUpAttachmentExists {
		t.Fatalf("queued attachments after clear: head=%t follow_up=%t, want true/false", headAttachmentExists, followUpAttachmentExists)
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

func TestPendingChatTaskAggregatesIncludeDeferredRetry(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	_, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "PendingDeferredAggregateAgent", []byte("[]"))
	sessionID := insertChatSessionAs(t, publicAgentID, memberID)
	taskID := insertPendingChatTask(t, publicAgentID, sessionID, "deferred")

	w := httptest.NewRecorder()
	testHandler.ListPendingChatTasks(w, chatPendingCtxAs(
		t,
		newRequestAs(memberID, "GET", "/api/chat/pending-tasks", nil),
		memberID,
	))
	if resp := decodePendingTasks(t, w); !containsPendingTask(resp.Tasks, taskID) {
		t.Fatalf("deferred retry %s missing from pending aggregate: %+v", taskID, resp.Tasks)
	}

	w = httptest.NewRecorder()
	testHandler.HasPendingChatTasks(w, chatPendingCtxAs(
		t,
		newRequestAs(memberID, "GET", "/api/chat/pending-tasks/has-any", nil),
		memberID,
	))
	if !decodeHasPending(t, w) {
		t.Fatal("has-any returned false for an accessible deferred retry")
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
