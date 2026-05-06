package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localmode"
)

func TestRuntimeHandlersRejectMalformedRuntimeID(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "usage",
			method: "GET",
			path:   "/api/runtimes/not-a-uuid/usage",
			handle: testHandler.GetRuntimeUsage,
		},
		{
			name:   "task activity",
			method: "GET",
			path:   "/api/runtimes/not-a-uuid/task-activity",
			handle: testHandler.GetRuntimeTaskActivity,
		},
		{
			name:   "delete",
			method: "DELETE",
			path:   "/api/runtimes/not-a-uuid",
			handle: testHandler.DeleteAgentRuntime,
		},
		{
			name:   "models",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/models",
			handle: testHandler.InitiateListModels,
		},
		{
			name:   "update",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/update",
			handle: testHandler.InitiateUpdate,
		},
		{
			name:   "local skills",
			method: "POST",
			path:   "/api/runtimes/not-a-uuid/local-skills",
			handle: testHandler.InitiateListLocalSkills,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest(tt.method, tt.path, nil)
			req = withURLParam(req, "runtimeId", "not-a-uuid")
			tt.handle(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400 for malformed runtimeId, got %d: %s", tt.name, w.Code, w.Body.String())
			}
		})
	}
}

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

	insertTaskWithUsage(yesterdayLate, todayEarly, 1000)          // cross-midnight
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

// newLocalRuntimeHandler returns a shallow clone of testHandler with LocalMode
// flipped on or off. Cloning avoids mutating shared global state across tests
// running in the same package.
func newLocalRuntimeHandler(localEnabled bool) *Handler {
	h := *testHandler
	if localEnabled {
		h.LocalMode = localmode.Config{ProductMode: "local"}
	} else {
		h.LocalMode = localmode.Config{}
	}
	return &h
}

func TestLocalGuardRuntime_HelperRejectsCloudInLocalMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	h := newLocalRuntimeHandler(true)

	w := httptest.NewRecorder()
	if h.rejectCloudRuntimeInLocalMode(w, "cloud") {
		t.Fatalf("expected helper to return false for cloud runtime in local mode")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cloud runtimes are unavailable") {
		t.Fatalf("expected error body to mention cloud runtimes, got %s", w.Body.String())
	}
}

func TestLocalGuardRuntime_HelperAllowsLocalInLocalMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	h := newLocalRuntimeHandler(true)

	w := httptest.NewRecorder()
	if !h.rejectCloudRuntimeInLocalMode(w, "local") {
		t.Fatalf("expected helper to return true for local runtime in local mode")
	}
	// Recorder default is 200 and body untouched.
	if w.Code != http.StatusOK {
		t.Fatalf("expected recorder code untouched (200), got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %s", w.Body.String())
	}
}

func TestLocalGuardRuntime_HelperAllowsCloudOutsideLocalMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	h := newLocalRuntimeHandler(false)

	w := httptest.NewRecorder()
	if !h.rejectCloudRuntimeInLocalMode(w, "cloud") {
		t.Fatalf("expected helper to return true when local mode is disabled")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected recorder code untouched (200), got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %s", w.Body.String())
	}
}

// seedLocalGuardRuntime inserts a runtime row in the test workspace with the
// supplied runtime_mode and provider. The provider is taken from the caller so
// concurrent tests don't collide on the (workspace_id, daemon_id, provider)
// unique constraint. The row is automatically deleted at test end.
func seedLocalGuardRuntime(t *testing.T, runtimeMode, provider string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, $3, $4, 'online', $5, '{}'::jsonb, now(), $6)
		RETURNING id
	`, testWorkspaceID, "Local Guard Test Runtime", runtimeMode, provider, "Local guard test runtime", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func TestLocalGuardRuntime_DeleteCloudRuntimeRejectedInLocalMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := seedLocalGuardRuntime(t, "cloud", "local_guard_test_delete_cloud")
	h := newLocalRuntimeHandler(true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	h.DeleteAgentRuntime(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cloud runtimes are unavailable") {
		t.Fatalf("expected error body to mention cloud runtimes, got %s", w.Body.String())
	}

	// The runtime row must still exist.
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&count); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected runtime row to remain, count=%d", count)
	}
}

func TestLocalGuardRuntime_DeleteLocalRuntimeAllowedInLocalMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := seedLocalGuardRuntime(t, "local", "local_guard_test_delete_local")
	h := newLocalRuntimeHandler(true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	h.DeleteAgentRuntime(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected ok status body, got %s", w.Body.String())
	}

	// The runtime row must be gone.
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&count); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected runtime row deleted, count=%d", count)
	}
}
