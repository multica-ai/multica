package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var f4IssueNumber int64

// f4SeedIssue inserts a minimal issue in the test workspace so
// ReportTaskMessages can resolve a workspace_id and exercise the realtime
// publish path. A monotonic issue number keeps the (workspace_id, number)
// unique constraint satisfied across test runs.
func f4SeedIssue(t *testing.T) string {
	t.Helper()
	num := atomic.AddInt64(&f4IssueNumber, 1) + 900000
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'f4 task-message test', 'todo', 'none', 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, num).Scan(&id); err != nil {
		t.Fatalf("seed f4 issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

func reportTaskMessagesHTTP(t *testing.T, taskID string, msgs []TaskMessageRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(TaskMessageBatchRequest{Messages: msgs})
	r := newRequest("POST", "/api/daemon/tasks/"+taskID+"/messages", json.RawMessage(body))
	r = withURLParam(r, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.ReportTaskMessages(w, r)
	return w
}

func countTaskMessages(t *testing.T, taskID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM task_message WHERE task_id = $1`, taskID,
	).Scan(&n); err != nil {
		t.Fatalf("count task_message: %v", err)
	}
	return n
}

// taskMessageEventCapture counts protocol.EventTaskMessage events published
// during a test, mirroring the comment event capture used by the resolve
// tests. Bus.Subscribe has no Unsubscribe, so the capture is process-global
// for the test run; each test tags its task id and filters on it.
type taskMessageEventCapture struct {
	mu     sync.Mutex
	seen   map[string]int
}

func captureTaskMessageEvents() *taskMessageEventCapture {
	cap := &taskMessageEventCapture{seen: make(map[string]int)}
	testHandler.Bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		cap.mu.Lock()
		cap.seen[e.TaskID]++
		cap.mu.Unlock()
	})
	return cap
}

func (c *taskMessageEventCapture) forTask(taskID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[taskID]
}

// TestReportTaskMessagesHappyPath verifies the transactional batch path
// persists every message and publishes a realtime event per row only after
// the commit succeeds.
func TestReportTaskMessagesHappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture unavailable (no database)")
	}
	agentID := createHandlerTestAgent(t, "f4-msg-happy", nil)
	issueID := f4SeedIssue(t)
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	cap := captureTaskMessageEvents()
	w := reportTaskMessagesHTTP(t, taskID, []TaskMessageRequest{
		{Seq: 1, Type: "text", Content: "first"},
		{Seq: 2, Type: "text", Content: "second"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if n := countTaskMessages(t, taskID); n != 2 {
		t.Errorf("persisted rows = %d, want 2", n)
	}
	if n := cap.forTask(taskID); n != 2 {
		t.Errorf("published events = %d, want 2 (publish must follow commit)", n)
	}
}

// TestReportTaskMessagesAtomicOnMidBatchFailure is the core F4 regression: a
// mid-batch DB error must roll back the whole batch instead of leaving a
// partial transcript, and must not broadcast any event for the rolled-back
// rows. The second message carries a NUL byte in Tool — Tool bypasses
// redaction and Postgres rejects 0x00 in text with an invalid-byte-sequence
// error, which deterministically fails the second INSERT while the first
// would otherwise succeed.
func TestReportTaskMessagesAtomicOnMidBatchFailure(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture unavailable (no database)")
	}
	agentID := createHandlerTestAgent(t, "f4-msg-atomic", nil)
	issueID := f4SeedIssue(t)
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	cap := captureTaskMessageEvents()
	w := reportTaskMessagesHTTP(t, taskID, []TaskMessageRequest{
		{Seq: 1, Type: "text", Content: "would-be-prefix"},
		{Seq: 2, Type: "text", Tool: "bad\x00tool", Content: "triggers invalid byte sequence"},
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if n := countTaskMessages(t, taskID); n != 0 {
		t.Errorf("persisted rows = %d, want 0 (whole batch must roll back)", n)
	}
	if n := cap.forTask(taskID); n != 0 {
		t.Errorf("published events = %d, want 0 (no event for rolled-back rows)", n)
	}
}

// TestReportTaskMessagesEmptyBatchIsNoop locks in the early return so the new
// transaction path is never opened for an empty payload.
func TestReportTaskMessagesEmptyBatchIsNoop(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture unavailable (no database)")
	}
	agentID := createHandlerTestAgent(t, "f4-msg-empty", nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)

	w := reportTaskMessagesHTTP(t, taskID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if n := countTaskMessages(t, taskID); n != 0 {
		t.Errorf("persisted rows = %d, want 0", n)
	}
}
