package handler

// CEREBRO-PATCH(chat-run-stream-test): FIR-2835 Phase 1 — tests for the SSE
// chat run stream endpoint (net-new cerebro sibling file).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/chatstream"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// withChatStreamBroker wires a fresh broker onto the shared test handler for
// the duration of one test.
func withChatStreamBroker(t *testing.T) {
	t.Helper()
	prev := testHandler.ChatStream
	testHandler.ChatStream = chatstream.NewBroker(testHandler.Bus)
	t.Cleanup(func() { testHandler.ChatStream = prev })
}

func insertChatTask(t *testing.T, sessionID, status string) string {
	t.Helper()
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT a.id, a.runtime_id FROM agent a
		 JOIN chat_session cs ON cs.agent_id = a.id
		 WHERE cs.id = $1`, sessionID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load agent/runtime: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			chat_session_id, created_at, started_at
		)
		VALUES ($1, $2, NULL, $3, 2, $4, now() - interval '3 seconds', now() - interval '2 seconds')
		RETURNING id
	`, agentID, runtimeID, status, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_message WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func streamRequest(sessionID, taskID string) *http.Request {
	path := "/api/chat/sessions/" + sessionID + "/stream"
	if taskID != "" {
		path += "?task_id=" + taskID
	}
	req := newChatRequest(http.MethodGet, path, testUserID, nil)
	return withURLParam(req, "sessionId", sessionID)
}

// sseFrames extracts the data payloads from an SSE body, skipping comments.
func sseFrames(body string) []string {
	var frames []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			frames = append(frames, strings.TrimPrefix(line, "data: "))
		}
	}
	return frames
}

func frameTypes(t *testing.T, frames []string) []string {
	t.Helper()
	var types []string
	for _, f := range frames {
		if f == "[DONE]" {
			types = append(types, "[DONE]")
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(f), &chunk); err != nil {
			t.Fatalf("frame not JSON: %q: %v", f, err)
		}
		typ, _ := chunk["type"].(string)
		types = append(types, typ)
	}
	return types
}

func TestStreamChatSessionRun_NoPendingTaskReturns204(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withChatStreamBroker(t)
	sessionID := createChatSessionForCoalesceTest(t)

	w := httptest.NewRecorder()
	testHandler.StreamChatSessionRun(w, streamRequest(sessionID, ""))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestStreamChatSessionRun_ReplaysCompletedTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withChatStreamBroker(t)
	ctx := context.Background()
	sessionID := createChatSessionForCoalesceTest(t)
	taskID := insertChatTask(t, sessionID, "running")

	// Complete through the real service so the assistant message + elapsed_ms
	// are persisted exactly as production writes them.
	result, _ := json.Marshal(protocol.TaskCompletedPayload{TaskID: taskID, Output: "det rigtige svar"})
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), result, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.StreamChatSessionRun(w, streamRequest(sessionID, taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := w.Header().Get(chatstream.UIMessageStreamHeader); got != chatstream.UIMessageStreamVersion {
		t.Errorf("%s = %q, want %q", chatstream.UIMessageStreamHeader, got, chatstream.UIMessageStreamVersion)
	}

	frames := sseFrames(w.Body.String())
	types := frameTypes(t, frames)
	want := []string{"start", "text-start", "text-delta", "text-end", "finish", "[DONE]"}
	if len(types) != len(want) {
		t.Fatalf("frame types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("frame %d = %s, want %s (all: %v)", i, types[i], want[i], types)
		}
	}
	if !strings.Contains(w.Body.String(), "det rigtige svar") {
		t.Errorf("stream body missing assistant content: %s", w.Body.String())
	}
	var finish struct {
		MessageMetadata struct {
			TaskID    string `json:"taskId"`
			ElapsedMs int64  `json:"elapsedMs"`
		} `json:"messageMetadata"`
	}
	if err := json.Unmarshal([]byte(frames[4]), &finish); err != nil {
		t.Fatalf("finish frame: %v", err)
	}
	if finish.MessageMetadata.TaskID != taskID {
		t.Errorf("finish.messageMetadata.taskId = %q, want %q", finish.MessageMetadata.TaskID, taskID)
	}
	if finish.MessageMetadata.ElapsedMs <= 0 {
		t.Errorf("finish.messageMetadata.elapsedMs = %d, want > 0", finish.MessageMetadata.ElapsedMs)
	}
}

func TestStreamChatSessionRun_LiveChatDoneFinishesStream(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withChatStreamBroker(t)
	sessionID := createChatSessionForCoalesceTest(t)
	taskID := insertChatTask(t, sessionID, "running")

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		testHandler.StreamChatSessionRun(w, streamRequest(sessionID, taskID))
		close(done)
	}()

	// The handler subscribes asynchronously; publish until it has consumed a
	// done event and returned. Repeated publishes are harmless — the handler
	// stops at the first match and later events hit a cancelled subscription.
	deadline := time.After(5 * time.Second)
	for {
		testHandler.Bus.Publish(events.Event{
			Type:          protocol.EventChatDone,
			WorkspaceID:   testWorkspaceID,
			ChatSessionID: sessionID,
			Payload: protocol.ChatDonePayload{
				ChatSessionID: sessionID,
				TaskID:        taskID,
				MessageID:     "live-msg-1",
				Content:       "live streamed answer",
				ElapsedMs:     750,
			},
		})
		select {
		case <-done:
		case <-deadline:
			t.Fatal("stream did not finish after chat:done")
		case <-time.After(10 * time.Millisecond):
			continue
		}
		break
	}

	body := w.Body.String()
	if !strings.Contains(body, "live streamed answer") {
		t.Errorf("stream missing live content: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("stream missing [DONE] terminator: %s", body)
	}
}

func TestStreamChatSessionRun_FailedTaskEmitsRealError(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withChatStreamBroker(t)
	ctx := context.Background()
	sessionID := createChatSessionForCoalesceTest(t)
	taskID := insertChatTask(t, sessionID, "running")

	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), "workspace_id is required", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.StreamChatSessionRun(w, streamRequest(sessionID, taskID))

	frames := sseFrames(w.Body.String())
	if len(frames) < 2 {
		t.Fatalf("expected error + [DONE] frames, got %v", frames)
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(frames[0]), &chunk); err != nil {
		t.Fatalf("error frame: %v", err)
	}
	if chunk["type"] != "error" {
		t.Fatalf("first frame type = %v, want error (frames: %v)", chunk["type"], frames)
	}
	errorText, _ := chunk["errorText"].(string)
	// The whole point of the stream contract: the REAL failure surfaces,
	// not a blanket "could not complete" string.
	if !strings.Contains(errorText, "workspace_id is required") {
		t.Errorf("errorText = %q, want the real failure text", errorText)
	}
	if frames[len(frames)-1] != "[DONE]" {
		t.Errorf("stream not terminated: %v", frames)
	}
}

func TestStreamChatSessionRun_RejectsForeignSessionTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withChatStreamBroker(t)
	sessionA := createChatSessionForCoalesceTest(t)
	sessionB := createChatSessionForCoalesceTest(t)
	taskB := insertChatTask(t, sessionB, "running")

	w := httptest.NewRecorder()
	testHandler.StreamChatSessionRun(w, streamRequest(sessionA, taskB))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a task from another session", w.Code)
	}
}

func TestStreamChatSessionRun_ForbiddenForNonCreator(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withChatStreamBroker(t)
	sessionID := createChatSessionForCoalesceTest(t)
	otherUser := createWorkspaceMemberUser(t, "member")

	req := newChatRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/stream", otherUser, nil)
	req = withURLParam(req, "sessionId", sessionID)
	w := httptest.NewRecorder()
	testHandler.StreamChatSessionRun(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-creator", w.Code)
	}
}
