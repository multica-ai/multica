package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type reworkDispatchHTTPResponse struct {
	Task struct {
		ID string `json:"id"`
	} `json:"task"`
	Replayed bool `json:"replayed"`
}

func dispatchReworkRequest(t *testing.T, userID, issueID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequestAs(userID, http.MethodPost, "/api/issues/"+issueID+"/rework-dispatch", body)
	return testutil.Call(t, testHandler.DispatchRework, testutil.WithURLParams(req, "id", issueID))
}

func getReworkDispatchRequest(t *testing.T, userID, issueID, key string) *testutil.Response {
	t.Helper()
	req := newRequestAs(userID, http.MethodGet, "/api/issues/"+issueID+"/rework-dispatch/"+key, nil)
	return testutil.Call(t, testHandler.GetReworkDispatch, testutil.WithURLParams(req, "id", issueID, "operationKey", key))
}

func decodeReworkDispatch(t *testing.T, w *testutil.Response) reworkDispatchHTTPResponse {
	t.Helper()
	var out reworkDispatchHTTPResponse
	w.JSON(&out)
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
	first.Want(http.StatusAccepted)
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
	replay.Want(http.StatusAccepted)
	replayed := decodeReworkDispatch(t, replay)
	if !replayed.Replayed || replayed.Task.ID != created.Task.ID {
		t.Fatalf("replayed dispatch = %#v, want task %s", replayed, created.Task.ID)
	}

	lookup := getReworkDispatchRequest(t, testUserID, issueID, "http-correction-v1")
	lookup.Want(http.StatusOK)
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
	dispatchReworkRequest(t, testUserID, issueID, mismatch).Want(http.StatusConflict)
	otherAgentID := createHandlerTestAgent(t, "rework-http-other-agent", nil)
	agentMismatch := map[string]any{
		"operation_key":  "http-correction-v1",
		"agent_id":       otherAgentID,
		"source_task_id": sourceTaskID,
		"instruction":    instruction,
		"payload_sha256": digest,
	}
	dispatchReworkRequest(t, testUserID, issueID, agentMismatch).Want(http.StatusConflict)
	badDigest := map[string]any{
		"operation_key":  "bad-digest",
		"agent_id":       agentID,
		"source_task_id": sourceTaskID,
		"instruction":    instruction,
		"payload_sha256": "not-a-sha256",
	}
	dispatchReworkRequest(t, testUserID, issueID, badDigest).Want(http.StatusBadRequest)
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
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/rework-dispatch/http-correction-v1", nil)
	req.Header.Set("X-Workspace-ID", otherWorkspaceID)
	testutil.Call(t, testHandler.GetReworkDispatch, testutil.WithURLParams(req, "id", issueID, "operationKey", "http-correction-v1")).Want(http.StatusNotFound)

	if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, created.Task.ID); err != nil {
		t.Fatalf("delete recorded task: %v", err)
	}
	getReworkDispatchRequest(t, testUserID, issueID, "http-correction-v1").Want(http.StatusGone)
	dispatchReworkRequest(t, testUserID, issueID, body).Want(http.StatusGone)
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
	dispatchReworkRequest(t, testUserID, issueID, body).Want(http.StatusAccepted)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("revoke invoke permission: %v", err)
	}
	dispatchReworkRequest(t, testUserID, issueID, body).Want(http.StatusForbidden)
	denied := getReworkDispatchRequest(t, testUserID, issueID, "permission-recheck").Want(http.StatusNotFound)
	unknown := getReworkDispatchRequest(t, testUserID, issueID, "unknown-rework-key").Want(http.StatusNotFound)
	if denied.Text() != unknown.Text() {
		t.Fatalf("denied and unknown GET responses differ: denied=%s unknown=%s", denied.Text(), unknown.Text())
	}
	// The private agent's owner remains allowed after the other member's grant
	// is revoked, proving the deny is an invoke re-check rather than receipt loss.
	getReworkDispatchRequest(t, ownerID, issueID, "permission-recheck").Want(http.StatusOK)
}

func reworkPayloadSHA256ForHandler(instruction string) string {
	// Keep the HTTP body tied to the immutable server-side package digest.
	sum := sha256.Sum256([]byte(instruction))
	return hex.EncodeToString(sum[:])
}
