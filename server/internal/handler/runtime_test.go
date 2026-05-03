package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGetRuntimeUsage_BucketsByUsageTime ensures a task that was enqueued on
// one calendar day but whose tokens were reported the next day (e.g. execution
// crossed midnight, or the task sat in the queue) is attributed to the day
// tokens were actually produced, not the enqueue day. It also verifies the
// ?days=N cutoff covers the full earliest calendar day, not just "now minus N
// days" which would clip the morning of that day.
func TestGetRuntimeUsage_BucketsByUsageTime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Pick a runtime bound to the fixture workspace.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	// Create an issue for the tasks to reference.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'runtime usage test', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// enqueued yesterday 23:58 UTC, finished today 00:05 UTC — tokens belong to today.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterdayLate := today.Add(-2 * time.Minute)
	todayEarly := today.Add(5 * time.Minute)
	// Task that ran entirely yesterday around 05:00 — used to verify the
	// ?days cutoff isn't clipping yesterday's morning.
	yesterdayMorning := today.Add(-19 * time.Hour)

	insertTaskWithUsage := func(enqueueAt, usageAt time.Time, inputTokens int64) string {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
			VALUES ($1, $2, $3, 'completed', $4)
			RETURNING id
		`, agentID, issueID, runtimeID, enqueueAt).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', 'claude-3-5-sonnet', $2, 0, $3)
		`, taskID, inputTokens, usageAt); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
		return taskID
	}

	insertTaskWithUsage(yesterdayLate, todayEarly, 1000)     // cross-midnight
	insertTaskWithUsage(yesterdayMorning, yesterdayMorning, 2000) // full-day yesterday

	// Call the handler with ?days=1 at whatever "now" is. That should include
	// both today and yesterday in full.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/runtimes/"+runtimeID+"/usage?days=1", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.GetRuntimeUsage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetRuntimeUsage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []RuntimeUsageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byDate := make(map[string]int64)
	for _, r := range resp {
		byDate[r.Date] += r.InputTokens
	}

	todayKey := today.Format("2006-01-02")
	yesterdayKey := today.Add(-24 * time.Hour).Format("2006-01-02")

	// Cross-midnight task must attribute to today (tu.created_at), not yesterday
	// (atq.created_at). Before the fix this was 0 on today / 1000 on yesterday.
	if byDate[todayKey] != 1000 {
		t.Errorf("cross-midnight task: today bucket expected 1000 input tokens, got %d (full map: %v)", byDate[todayKey], byDate)
	}
	// Yesterday's morning task must still be included — this is what breaks
	// when ?days=N is interpreted as a rolling window instead of calendar days.
	if byDate[yesterdayKey] != 2000 {
		t.Errorf("yesterday morning task: yesterday bucket expected 2000 input tokens, got %d (full map: %v)", byDate[yesterdayKey], byDate)
	}
}

// callUpdateSandbox sends a PATCH request to the sandbox endpoint and returns
// the recorded response. Uses any-typed body so callers can pass typed bools,
// nil, or raw bytes for malformed-body tests.
func callUpdateSandbox(t *testing.T, runtimeID string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if raw, ok := body.([]byte); ok {
		req = httptest.NewRequest(http.MethodPatch, "/api/runtimes/"+runtimeID+"/sandbox", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
	} else {
		req = newRequest(http.MethodPatch, "/api/runtimes/"+runtimeID+"/sandbox", body)
	}
	req = withURLParam(req, "runtimeId", runtimeID)

	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntimeSandbox(w, req)
	return w
}

// TestUpdateRuntimeSandbox_AdminTogglePersists drives the JEH-418 acceptance
// criterion: admin sets sandbox_enabled=false, response reflects it, the
// runtime row persists it, and a follow-up null clears the override back to
// inheriting the daemon's env-var default.
func TestUpdateRuntimeSandbox_AdminTogglePersists(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	t.Cleanup(func() {
		// Reset to NULL so other tests aren't affected by leftover state.
		testPool.Exec(ctx, `UPDATE agent_runtime SET sandbox_enabled = NULL WHERE id = $1`, runtimeID)
	})

	// Override to false.
	disabled := false
	w := callUpdateSandbox(t, runtimeID, map[string]any{"sandbox_enabled": &disabled})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntimeSandbox(false): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SandboxEnabled == nil || *resp.SandboxEnabled != false {
		t.Fatalf("response sandbox_enabled = %v, want false", resp.SandboxEnabled)
	}

	// Persisted in DB?
	var dbValid bool
	var dbVal bool
	if err := testPool.QueryRow(ctx,
		`SELECT sandbox_enabled IS NOT NULL, COALESCE(sandbox_enabled, false) FROM agent_runtime WHERE id = $1`,
		runtimeID,
	).Scan(&dbValid, &dbVal); err != nil {
		t.Fatalf("query persisted value: %v", err)
	}
	if !dbValid || dbVal != false {
		t.Fatalf("persisted: valid=%v val=%v, want valid=true val=false", dbValid, dbVal)
	}

	// Override to true.
	enabled := true
	w = callUpdateSandbox(t, runtimeID, map[string]any{"sandbox_enabled": &enabled})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntimeSandbox(true): expected 200, got %d", w.Code)
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SandboxEnabled == nil || *resp.SandboxEnabled != true {
		t.Fatalf("response sandbox_enabled = %v, want true", resp.SandboxEnabled)
	}

	// Clear override (null).
	w = callUpdateSandbox(t, runtimeID, map[string]any{"sandbox_enabled": nil})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntimeSandbox(null): expected 200, got %d", w.Code)
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SandboxEnabled != nil {
		t.Fatalf("response sandbox_enabled = %v, want nil after clearing override", resp.SandboxEnabled)
	}
}

// TestUpdateRuntimeSandbox_MemberForbidden ensures non-admins cannot flip the
// sandbox setting — it's a security-posture toggle gated to owner/admin.
func TestUpdateRuntimeSandbox_MemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	// Demote testUserID to "member" for the duration of this test.
	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("demote to member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})

	disabled := false
	w := callUpdateSandbox(t, runtimeID, map[string]any{"sandbox_enabled": &disabled})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for member, got %d: %s", w.Code, w.Body.String())
	}

	// Sanity: nothing persisted to the runtime row.
	var dbValid bool
	if err := testPool.QueryRow(ctx,
		`SELECT sandbox_enabled IS NOT NULL FROM agent_runtime WHERE id = $1`,
		runtimeID,
	).Scan(&dbValid); err != nil {
		t.Fatalf("query persisted value: %v", err)
	}
	if dbValid {
		t.Fatalf("expected no override persisted after forbidden request")
	}
}

// TestUpdateRuntimeSandbox_RejectsMalformedBody guards against silently
// treating an unparseable body as an explicit null.
func TestUpdateRuntimeSandbox_RejectsMalformedBody(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	w := callUpdateSandbox(t, runtimeID, []byte("not-json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimResponseSurfacesSandboxOverride ensures the daemon-facing claim
// response carries the runtime's per-runtime sandbox setting so the daemon
// can honour it without restart.
func TestClaimResponseSurfacesSandboxOverride(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET sandbox_enabled = NULL WHERE id = $1`, runtimeID)
	})

	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET sandbox_enabled = false WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("set override: %v", err)
	}

	// Set up an issue + queued task for the runtime so claim has something to return.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'sandbox claim test', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("find agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var claim struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&claim); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claim.Task == nil {
		t.Fatalf("expected task in claim response, got nil")
	}
	if claim.Task.SandboxEnabled == nil {
		t.Fatalf("expected sandbox_enabled in claim response, got nil — daemon would fall back to env-var default and miss the override")
	}
	if *claim.Task.SandboxEnabled != false {
		t.Fatalf("expected sandbox_enabled=false, got %v", *claim.Task.SandboxEnabled)
	}
}
