package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type reworkDispatchHTTPResponse struct {
	Task struct {
		ID string `json:"id"`
	} `json:"task"`
	Replayed bool `json:"replayed"`
}

func dispatchReworkRequest(t *testing.T, userID, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, http.MethodPost, "/api/issues/"+issueID+"/rework-dispatch", body)
	testHandler.DispatchRework(w, withURLParam(req, "id", issueID))
	return w
}

func getReworkDispatchRequest(t *testing.T, userID, issueID, key string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, http.MethodGet, "/api/issues/"+issueID+"/rework-dispatch/"+key, nil)
	testHandler.GetReworkDispatch(w, withURLParams(req, "id", issueID, "operationKey", key))
	return w
}

func decodeReworkDispatch(t *testing.T, w *httptest.ResponseRecorder) reworkDispatchHTTPResponse {
	t.Helper()
	var out reworkDispatchHTTPResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode rework dispatch response: %v: %s", err, w.Body.String())
	}
	return out
}

func seedTerminalReworkSource(t *testing.T, agentID, issueID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, completed_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'completed', 0, now())
		RETURNING id
	`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed terminal source task: %v", err)
	}
	return taskID
}

// TestReworkDispatchHTTPIdempotencyAndBoundaries exercises the public handler
// contract against the real queue and authorization checks. In particular, a
// receipt is not a capability: replay and lookup re-evaluate invoke rights.
func TestReworkDispatchHTTPIdempotencyAndBoundaries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("handler test database is required")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "rework-http-agent", nil)
	issueID := insertAgentAssignedIssue(t, agentID, 994201, "rework HTTP contract")
	sourceTaskID := seedTerminalReworkSource(t, agentID, issueID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_rework_dispatch_receipt WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	instruction := "Apply the approved corrective package."
	digest := reworkPayloadSHA256ForHandler(instruction)
	body := map[string]any{
		"operation_key":  "http-correction-v1",
		"agent_id":       agentID,
		"source_task_id": sourceTaskID,
		"instruction":    instruction,
		"payload_sha256": digest,
	}
	first := dispatchReworkRequest(t, testUserID, issueID, body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first dispatch: got %d: %s", first.Code, first.Body.String())
	}
	created := decodeReworkDispatch(t, first)
	if created.Replayed || created.Task.ID == "" {
		t.Fatalf("first dispatch = %#v, want a new task", created)
	}
	var sourceStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, sourceTaskID).Scan(&sourceStatus); err != nil || sourceStatus != "completed" {
		t.Fatalf("source task was not preserved: status=%q err=%v", sourceStatus, err)
	}
	var rerunOfTaskID string
	if err := testPool.QueryRow(ctx, `SELECT rerun_of_task_id::text FROM agent_task_queue WHERE id = $1`, created.Task.ID).Scan(&rerunOfTaskID); err != nil || rerunOfTaskID != sourceTaskID {
		t.Fatalf("dispatched task did not retain its source run: rerun_of_task_id=%q err=%v", rerunOfTaskID, err)
	}

	replay := dispatchReworkRequest(t, testUserID, issueID, body)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replayed dispatch: got %d: %s", replay.Code, replay.Body.String())
	}
	replayed := decodeReworkDispatch(t, replay)
	if !replayed.Replayed || replayed.Task.ID != created.Task.ID {
		t.Fatalf("replayed dispatch = %#v, want task %s", replayed, created.Task.ID)
	}

	lookup := getReworkDispatchRequest(t, testUserID, issueID, "http-correction-v1")
	if lookup.Code != http.StatusOK {
		t.Fatalf("dispatch lookup: got %d: %s", lookup.Code, lookup.Body.String())
	}
	if got := decodeReworkDispatch(t, lookup).Task.ID; got != created.Task.ID {
		t.Fatalf("lookup task = %s, want %s", got, created.Task.ID)
	}

	mismatch := map[string]any{
		"operation_key":  "http-correction-v1",
		"agent_id":       agentID,
		"source_task_id": sourceTaskID,
		"instruction":    "A different package must not reuse this key.",
		"payload_sha256": reworkPayloadSHA256ForHandler("A different package must not reuse this key."),
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, mismatch); w.Code != http.StatusConflict {
		t.Fatalf("payload mismatch: got %d: %s", w.Code, w.Body.String())
	}
	otherAgentID := createHandlerTestAgent(t, "rework-http-other-agent", nil)
	agentMismatch := map[string]any{
		"operation_key":  "http-correction-v1",
		"agent_id":       otherAgentID,
		"source_task_id": sourceTaskID,
		"instruction":    instruction,
		"payload_sha256": digest,
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, agentMismatch); w.Code != http.StatusConflict {
		t.Fatalf("agent mismatch: got %d: %s", w.Code, w.Body.String())
	}
	badDigest := map[string]any{
		"operation_key":  "bad-digest",
		"agent_id":       agentID,
		"source_task_id": sourceTaskID,
		"instruction":    instruction,
		"payload_sha256": "not-a-sha256",
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, badDigest); w.Code != http.StatusBadRequest {
		t.Fatalf("bad digest: got %d: %s", w.Code, w.Body.String())
	}
	active := map[string]any{
		"operation_key":  "http-correction-v2",
		"agent_id":       agentID,
		"source_task_id": sourceTaskID,
		"instruction":    "A distinct correction while the prior run is active.",
		"payload_sha256": reworkPayloadSHA256ForHandler("A distinct correction while the prior run is active."),
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, active); w.Code != http.StatusConflict {
		t.Fatalf("active work conflict: got %d: %s", w.Code, w.Body.String())
	}

	// The issue belongs to the normal fixture workspace. Supplying a different
	// real workspace header must not reveal or dispatch it.
	otherWorkspaceID := dbfx.Workspace(t, "Rework other workspace", "rework-other-workspace")
	dbfx.Member(t, otherWorkspaceID, testUserID, "member")
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/rework-dispatch/http-correction-v1", nil)
	req.Header.Set("X-Workspace-ID", otherWorkspaceID)
	testHandler.GetReworkDispatch(w, withURLParams(req, "id", issueID, "operationKey", "http-correction-v1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace lookup: got %d: %s", w.Code, w.Body.String())
	}

	if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, created.Task.ID); err != nil {
		t.Fatalf("delete recorded task: %v", err)
	}
	if w := getReworkDispatchRequest(t, testUserID, issueID, "http-correction-v1"); w.Code != http.StatusGone {
		t.Fatalf("lookup missing recorded task: got %d: %s", w.Code, w.Body.String())
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, body); w.Code != http.StatusGone {
		t.Fatalf("replay missing recorded task: got %d: %s", w.Code, w.Body.String())
	}
}

func TestReworkDispatchHTTPReplayRechecksInvokePermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("handler test database is required")
	}
	ctx := context.Background()
	agentID, ownerID, _ := privateAgentTestFixture(t)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("make private agent initially invocable: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'member', $2)`, agentID, testUserID); err != nil {
		t.Fatalf("grant initial invoke permission: %v", err)
	}
	issueID := insertAgentAssignedIssue(t, agentID, 994202, "rework replay permission")
	sourceTaskID := seedTerminalReworkSource(t, agentID, issueID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_rework_dispatch_receipt WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})
	instruction := "Dispatch while permission is present."
	body := map[string]any{
		"operation_key":  "permission-recheck",
		"agent_id":       agentID,
		"source_task_id": sourceTaskID,
		"instruction":    instruction,
		"payload_sha256": reworkPayloadSHA256ForHandler(instruction),
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, body); w.Code != http.StatusAccepted {
		t.Fatalf("initial permitted dispatch: got %d: %s", w.Code, w.Body.String())
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("revoke invoke permission: %v", err)
	}
	if w := dispatchReworkRequest(t, testUserID, issueID, body); w.Code != http.StatusForbidden {
		t.Fatalf("replay after revoke: got %d: %s", w.Code, w.Body.String())
	}
	if w := getReworkDispatchRequest(t, testUserID, issueID, "permission-recheck"); w.Code != http.StatusForbidden {
		t.Fatalf("lookup after revoke: got %d: %s", w.Code, w.Body.String())
	}
	// The private agent's owner remains allowed after the other member's grant
	// is revoked, proving the deny is an invoke re-check rather than receipt loss.
	if w := getReworkDispatchRequest(t, ownerID, issueID, "permission-recheck"); w.Code != http.StatusOK {
		t.Fatalf("lookup as private-agent owner: got %d: %s", w.Code, w.Body.String())
	}
}

func reworkPayloadSHA256ForHandler(instruction string) string {
	// Keep the HTTP body tied to the immutable server-side package digest.
	sum := sha256.Sum256([]byte(instruction))
	return hex.EncodeToString(sum[:])
}
