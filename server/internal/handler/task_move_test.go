package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// MoveTaskByUser (POST /api/tasks/{taskId}/move) reorders one queued task one
// step up/down in its agent's claim order (priority DESC, created_at ASC,
// id ASC). These tests pin the reorder semantics on the coarse 0–4 priority
// scale (take the neighbor's level across levels, step one level on a
// same-level tie, no-op at the queue edge) and the tenancy/privacy gates
// copied from CancelTaskByUser.

// moveTaskByUserRequest builds a /move request the way the router delivers
// one: member actor, JSON body, taskId URL param, test workspace context.
func moveTaskByUserRequest(t *testing.T, userID, taskID, direction string) *http.Request {
	t.Helper()
	req := newRequestAs(userID, http.MethodPost, "/api/tasks/"+taskID+"/move", map[string]string{
		"direction": direction,
	})
	req = withURLParam(req, "taskId", taskID)
	return withChatTestWorkspaceCtx(t, req)
}

func moveTaskPriority(t *testing.T, taskID string) int {
	t.Helper()
	var priority int
	dbfx.QueryRow(t,
		`SELECT priority FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&priority)
	return priority
}

func decodeMoveTaskResponse(t *testing.T, w *httptest.ResponseRecorder) MoveTaskResponse {
	t.Helper()
	var response MoveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode move task response: %v", err)
	}
	return response
}

// seedMoveQueueTask queues a task for agentID at an explicit priority and
// created_at so the claim order (priority DESC, created_at ASC, id ASC) is
// fully deterministic — gen_random_uuid() ids make the id tie-break
// arbitrary, and now() timestamps can collide within one test.
func seedMoveQueueTask(t *testing.T, agentID string, priority int, createdOffset string) string {
	t.Helper()
	return dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"priority":   priority,
		"created_at": testutil.Raw("now() - interval '" + createdOffset + "'"),
	})
}

func TestMoveTaskByUser_UpStepsTowardFront(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveUpAgent", []byte("[]"))
	// Claim order: A (p2) → B (p1) → C (p0).
	seedMoveQueueTask(t, agentID, 2, "3 seconds")
	taskB := seedMoveQueueTask(t, agentID, 1, "2 seconds")
	taskC := seedMoveQueueTask(t, agentID, 0, "1 second")

	// C takes its neighbor B's level: 0 → 1.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskC, "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	response := decodeMoveTaskResponse(t, w)
	if response.TaskID != taskC || !response.Moved {
		t.Fatalf("response = %+v, want task %s moved", response, taskC)
	}
	if response.Priority != 1 || moveTaskPriority(t, taskC) != 1 {
		t.Fatalf("task C priority = %d (db %d), want 1", response.Priority, moveTaskPriority(t, taskC))
	}

	// B takes its neighbor A's level: 1 → 2.
	w = httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskB, "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); !got.Moved || got.Priority != 2 {
		t.Fatalf("response = %+v, want moved to priority 2", got)
	}

	// A is at the front: no-op, moved=false, priority unchanged.
	var frontID string
	dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE agent_id = $1 AND priority = 2 ORDER BY created_at ASC LIMIT 1`, agentID).Scan(&frontID)
	w = httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, frontID, "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); got.Moved || got.Priority != 2 {
		t.Fatalf("front-of-queue move = %+v, want moved=false at priority 2", got)
	}
}

func TestMoveTaskByUser_UpSameLevelStepsOneLevel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveUpSameLevelAgent", []byte("[]"))
	seedMoveQueueTask(t, agentID, 1, "2 seconds")
	taskB := seedMoveQueueTask(t, agentID, 1, "1 second")

	// Same-level peer ahead: bump one level, 1 → 2.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskB, "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); !got.Moved || got.Priority != 2 {
		t.Fatalf("response = %+v, want moved to priority 2", got)
	}
	if got := moveTaskPriority(t, taskB); got != 2 {
		t.Fatalf("db priority = %d, want 2", got)
	}
}

func TestMoveTaskByUser_UpClampedAtTop(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveUpClampAgent", []byte("[]"))
	seedMoveQueueTask(t, agentID, 4, "2 seconds")
	taskB := seedMoveQueueTask(t, agentID, 4, "1 second")

	// Already at the top level with a same-level peer ahead: nothing to give
	// it — no-op, moved=false.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskB, "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); got.Moved || got.Priority != 4 {
		t.Fatalf("clamped move = %+v, want moved=false at priority 4", got)
	}
	if got := moveTaskPriority(t, taskB); got != 4 {
		t.Fatalf("db priority = %d, want 4", got)
	}
}

func TestMoveTaskByUser_DownStepsTowardBack(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveDownAgent", []byte("[]"))
	// Claim order: A (p2) → B (p1) → C (p0).
	seedMoveQueueTask(t, agentID, 2, "3 seconds")
	taskB := seedMoveQueueTask(t, agentID, 1, "2 seconds")
	seedMoveQueueTask(t, agentID, 0, "1 second")

	// B takes its neighbor C's level: 1 → 0, falling behind C (older).
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskB, "down"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); !got.Moved || got.Priority != 0 {
		t.Fatalf("response = %+v, want moved to priority 0", got)
	}
	if got := moveTaskPriority(t, taskB); got != 0 {
		t.Fatalf("db priority = %d, want 0", got)
	}

	// C is at the back: no-op, moved=false, priority unchanged.
	var backID string
	dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE agent_id = $1 AND priority = 0 ORDER BY created_at DESC LIMIT 1`, agentID).Scan(&backID)
	w = httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, backID, "down"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); got.Moved || got.Priority != 0 {
		t.Fatalf("back-of-queue move = %+v, want moved=false at priority 0", got)
	}
}

func TestMoveTaskByUser_DownSameLevelStepsOneLevel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveDownSameLevelAgent", []byte("[]"))
	taskA := seedMoveQueueTask(t, agentID, 1, "2 seconds")
	seedMoveQueueTask(t, agentID, 1, "1 second")

	// Same-level peer behind: drop one level, 1 → 0.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskA, "down"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); !got.Moved || got.Priority != 0 {
		t.Fatalf("response = %+v, want moved to priority 0", got)
	}
}

func TestMoveTaskByUser_DownClampedAtBottom(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveDownClampAgent", []byte("[]"))
	taskA := seedMoveQueueTask(t, agentID, 0, "2 seconds")
	seedMoveQueueTask(t, agentID, 0, "1 second")

	// Already at the bottom level with a same-level peer behind: no-op.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskA, "down"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); got.Moved || got.Priority != 0 {
		t.Fatalf("clamped move = %+v, want moved=false at priority 0", got)
	}
}

func TestMoveTaskByUser_NonQueuedTaskConflicts(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveNonQueuedAgent", []byte("[]"))
	runningID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
	})

	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, runningID, "up"))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMoveTaskByUser_UnknownTaskNotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, "00000000-0000-0000-0000-00000000dead", "up"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMoveTaskByUser_ForeignWorkspaceTaskNotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createForeignWorkspaceAgent(t)
	taskID := createAutopilotRunOnlyTask(t, agentID)

	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskID, "up"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMoveTaskByUser_BadDirectionRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveBadDirectionAgent", []byte("[]"))
	taskID := seedMoveQueueTask(t, agentID, 0, "1 second")

	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskID, "left"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskID, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing direction, got %d: %s", w.Code, w.Body.String())
	}
	if got := moveTaskPriority(t, taskID); got != 0 {
		t.Fatalf("rejected move changed priority to %d", got)
	}
}

func TestMoveTaskByUser_PrivateAgentGate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID, memberID := privateAgentTestFixture(t)
	seedMoveQueueTask(t, agentID, 0, "1 second")

	// A plain member without access to the private agent is refused.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, memberID, firstTaskForAgent(t, agentID), "up"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// The agent owner is admitted.
	w = httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, ownerID, firstTaskForAgent(t, agentID), "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("agent owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMoveTaskByUser_ChatTaskCreatorOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "MoveChatAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	seedMoveQueueTask(t, agentID, 0, "2 seconds")
	taskB := insertPendingChatTask(t, agentID, sessionID, "queued")

	// The session creator (testUserID) may reorder their own chat task.
	w := httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, testUserID, taskB, "up"))
	if w.Code != http.StatusOK {
		t.Fatalf("creator: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMoveTaskResponse(t, w); !got.Moved || got.Priority != 1 {
		t.Fatalf("creator response = %+v, want moved to priority 1", got)
	}

	// A plain workspace member is refused.
	memberID := createWorkspaceMemberUser(t, "Move Chat Member", "move-chat-member@multica.test")
	w = httptest.NewRecorder()
	testHandler.MoveTaskByUser(w, moveTaskByUserRequest(t, memberID, taskB, "up"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-creator: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := moveTaskPriority(t, taskB); got != 1 {
		t.Fatalf("refused move changed priority to %d", got)
	}
}

// firstTaskForAgent returns the oldest queued task id for agentID.
func firstTaskForAgent(t *testing.T, agentID string) string {
	t.Helper()
	var taskID string
	dbfx.QueryRow(t,
		`SELECT id FROM agent_task_queue WHERE agent_id = $1 ORDER BY created_at ASC LIMIT 1`, agentID,
	).Scan(&taskID)
	return taskID
}
