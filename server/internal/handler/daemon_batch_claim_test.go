package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// batchClaimResponse mirrors the {"tasks":[...]} envelope ClaimTasksByRuntime
// returns, with the few fields these tests assert on.
type batchClaimResponse struct {
	Tasks []struct {
		ID        string `json:"id"`
		RuntimeID string `json:"runtime_id"`
		AuthToken string `json:"auth_token"`
	} `json:"tasks"`
}

func seedQueuedIssueTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&id); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id) })
	return id
}

func postBatchClaim(t *testing.T, workspaceID string, runtimeIDs []string, maxTasks int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/claim",
		map[string]any{"daemon_id": batchClaimTestDaemonID, "runtime_ids": runtimeIDs, "max_tasks": maxTasks},
		workspaceID, batchClaimTestDaemonID)
	testHandler.ClaimTasksByRuntime(w, req)
	return w
}

// batchClaimTestDaemonID is the daemon id used by both the mdt_ token context
// and the request body in batch-claim handler tests, so the daemon_id
// consistency check passes on the happy path.
const batchClaimTestDaemonID = "batch-claim-review"

// TestClaimTasksByRuntime_RoutesAcrossRuntimesAndMintsTokens covers the happy
// path: one call claims across two runtimes on the same machine, returns one
// task per runtime (per-agent dedup), and mints a task-scoped token for each.
func TestClaimTasksByRuntime_RoutesAcrossRuntimesAndMintsTokens(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	rt1 := createClaimReclaimRuntime(t, ctx, "Batch claim rt1")
	rt2 := createClaimReclaimRuntime(t, ctx, "Batch claim rt2")
	a1, i1 := createClaimReclaimAgentAndIssue(t, ctx, rt1, "Batch claim a1")
	a2, i2 := createClaimReclaimAgentAndIssue(t, ctx, rt2, "Batch claim a2")
	seedQueuedIssueTask(t, ctx, a1, rt1, i1)
	seedQueuedIssueTask(t, ctx, a2, rt2, i2)

	w := postBatchClaim(t, testWorkspaceID, []string{rt1, rt2}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("claimed %d tasks, want 2: %s", len(resp.Tasks), w.Body.String())
	}
	seen := map[string]int{}
	for _, task := range resp.Tasks {
		seen[task.RuntimeID]++
		if !strings.HasPrefix(task.AuthToken, "mat_") {
			t.Fatalf("task %s missing mat_ task token, got %q", task.ID, task.AuthToken)
		}
	}
	if seen[rt1] != 1 || seen[rt2] != 1 {
		t.Fatalf("runtime distribution = %v, want one task each for rt1/rt2", seen)
	}
}

func TestRuntimeTargetedBatchClaimDoesNotDispatchHeadOutsideRuntimeSet(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("database is required for Runtime-targeted batch handler tests")
	}
	ctx := context.Background()
	runtimeA := createClaimReclaimRuntime(t, ctx, "Runtime-targeted batch A")
	runtimeB := createClaimReclaimRuntime(t, ctx, "Runtime-targeted batch B")
	agentID, issueB := createClaimReclaimAgentAndIssue(t, ctx, runtimeB, "Runtime-targeted batch Agent")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET max_concurrent_tasks = 5 WHERE id = $1`, agentID); err != nil {
		t.Fatalf("raise Agent capacity: %v", err)
	}
	var issueA string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'Runtime-targeted batch lower A', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 960000) + 1 FROM issue WHERE workspace_id = $1), 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueA); err != nil {
		t.Fatalf("create Runtime-A Issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueA) })
	lowerA := seedQueuedIssueTask(t, ctx, agentID, runtimeA, issueA)
	headB := seedQueuedIssueTask(t, ctx, agentID, runtimeB, issueB)
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET priority = CASE id WHEN $1 THEN 1 ELSE 10 END WHERE id IN ($1,$2)`, lowerA, headB); err != nil {
		t.Fatalf("order Runtime-targeted Tasks: %v", err)
	}

	w := postBatchClaim(t, testWorkspaceID, []string{runtimeA}, 2)
	if w.Code != http.StatusOK {
		t.Fatalf("Runtime-A batch status = %d: %s", w.Code, w.Body.String())
	}
	var response batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode Runtime-A batch: %v", err)
	}
	if len(response.Tasks) != 0 {
		t.Fatalf("Runtime-A-only batch claimed other Runtime head: %+v", response.Tasks)
	}
	var statusA, statusB string
	if err := testPool.QueryRow(ctx, `
		SELECT max(status) FILTER (WHERE id = $1), max(status) FILTER (WHERE id = $2)
		FROM agent_task_queue WHERE id IN ($1,$2)
	`, lowerA, headB).Scan(&statusA, &statusB); err != nil {
		t.Fatalf("read preserved batch Task statuses: %v", err)
	}
	if statusA != "queued" || statusB != "queued" {
		t.Fatalf("wrong Runtime batch mutated Tasks: A=%s B=%s", statusA, statusB)
	}

	w = postBatchClaim(t, testWorkspaceID, []string{runtimeA, runtimeB}, 2)
	if w.Code != http.StatusOK {
		t.Fatalf("two-Runtime batch status = %d: %s", w.Code, w.Body.String())
	}
	response = batchClaimResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode two-Runtime batch: %v", err)
	}
	if len(response.Tasks) != 1 || response.Tasks[0].ID != headB || response.Tasks[0].RuntimeID != runtimeB {
		t.Fatalf("two-Runtime batch = %+v, want only Runtime-B global head %s", response.Tasks, headB)
	}
}

// TestClaimTasksByRuntime_SkipsCrossWorkspaceRuntime is the security-critical
// case: a daemon token scoped to workspace A must not claim a task routed to a
// runtime in workspace B, even when B's runtime_id is included in the request.
func TestClaimTasksByRuntime_SkipsCrossWorkspaceRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A foreign workspace with its own runtime + agent + queued task.
	var foreignUser, foreignWS string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Foreign User', 'batch-foreign@multica.ai') RETURNING id`).Scan(&foreignUser); err != nil {
		t.Fatalf("foreign user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, foreignUser) })
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Foreign WS','batch-foreign-ws','x','FGN') RETURNING id`).Scan(&foreignWS); err != nil {
		t.Fatalf("foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWS) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, foreignWS, foreignUser); err != nil {
		t.Fatalf("foreign member: %v", err)
	}
	var foreignRT, foreignAgent, foreignIssue string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'Foreign RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, foreignWS, foreignUser).Scan(&foreignRT); err != nil {
		t.Fatalf("foreign runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Foreign Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id`, foreignWS, foreignRT, foreignUser).Scan(&foreignAgent); err != nil {
		t.Fatalf("foreign agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'foreign issue', 'in_progress', 'none', $2, 'member', 1, 0)
		RETURNING id`, foreignWS, foreignUser).Scan(&foreignIssue); err != nil {
		t.Fatalf("foreign issue: %v", err)
	}
	foreignTask := seedQueuedIssueTask(t, ctx, foreignAgent, foreignRT, foreignIssue)

	// Daemon token scoped to the (unrelated) handler-test workspace.
	w := postBatchClaim(t, testWorkspaceID, []string{foreignRT}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("cross-workspace claim leaked %d tasks, want 0: %s", len(resp.Tasks), w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, foreignTask).Scan(&status); err != nil {
		t.Fatalf("read foreign task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("foreign task status = %s, want still queued (untouched)", status)
	}
}

// TestClaimTasksByRuntime_CancelsTaskWhenRuntimeOwnerMissing pins the
// unscoped-credential guard: a runtime with no owner cannot mint a task token,
// so the claimed task must be cancelled and omitted from the response rather
// than shipped without a scoped credential.
func TestClaimTasksByRuntime_CancelsTaskWhenRuntimeOwnerMissing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var rtNull string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'Ownerless RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', NULL)
		RETURNING id`, testWorkspaceID).Scan(&rtNull); err != nil {
		t.Fatalf("ownerless runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rtNull) })

	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rtNull, "Ownerless agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rtNull, issueID)

	w := postBatchClaim(t, testWorkspaceID, []string{rtNull}, 1)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("claimed %d tasks from owner-less runtime, want 0: %s", len(resp.Tasks), w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("task status = %s, want cancelled (owner missing)", status)
	}
}
