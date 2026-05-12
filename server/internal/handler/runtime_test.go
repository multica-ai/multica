package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

	// ListRuntimeUsage now reads from the `task_usage_daily` rollup
	// table maintained by the cron-driven rollup_task_usage_daily()
	// function. In production the watermarked wrapper waits a 5 min
	// safety lag before consuming rows; here we drive the underlying
	// window function directly with a wide-open range so the freshly
	// inserted fixture rows are guaranteed to be aggregated before the
	// handler is called. Each test invocation gets its own isolated
	// daily buckets keyed by (date, runtime, provider, model), so
	// re-running the test is idempotent (the upsert just rewrites the
	// same totals).
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)
	`); err != nil {
		t.Fatalf("rollup_task_usage_daily_window: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date IN ($2::date, $3::date)
		`, runtimeID, today, today.Add(-24*time.Hour))
	})

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

func TestGetRuntimeUsageDailyRollupCutoffUsesRuntimeTimezone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	cutoff := time.Date(2026, 5, 4, 0, 0, 0, 0, loc)
	cutoffDate := cutoff.Format("2006-01-02")
	extraDate := cutoff.AddDate(0, 0, -1).Format("2006-01-02")

	var originalTZ string
	if err := testPool.QueryRow(ctx, `SELECT timezone FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&originalTZ); err != nil {
		t.Fatalf("read runtime timezone: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET timezone = $1 WHERE id = $2`, originalTZ, runtimeID)
		testPool.Exec(ctx, `DELETE FROM task_usage_daily WHERE runtime_id = $1 AND provider = 'cutoff-test'`, runtimeID)
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET timezone = 'Asia/Shanghai' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("set runtime timezone: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage_daily (
			bucket_date, workspace_id, runtime_id, provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, event_count
		)
		VALUES
			($1::date, $3, $4, 'cutoff-test', 'old-day', 111, 0, 0, 0, 1),
			($2::date, $3, $4, 'cutoff-test', 'cutoff-day', 222, 0, 0, 0, 1)
		ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
			SET input_tokens = EXCLUDED.input_tokens,
			    output_tokens = EXCLUDED.output_tokens,
			    cache_read_tokens = EXCLUDED.cache_read_tokens,
			    cache_write_tokens = EXCLUDED.cache_write_tokens,
			    event_count = EXCLUDED.event_count
	`, extraDate, cutoffDate, testWorkspaceID, runtimeID); err != nil {
		t.Fatalf("seed rollup rows: %v", err)
	}

	origRollup := testHandler.cfg.UseDailyRollupForRuntimeUsage
	testHandler.cfg.UseDailyRollupForRuntimeUsage = true
	t.Cleanup(func() { testHandler.cfg.UseDailyRollupForRuntimeUsage = origRollup })

	resp, err := testHandler.listRuntimeUsage(ctx, parseUUID(runtimeID), "Asia/Shanghai", pgtype.Timestamptz{
		Time:  cutoff,
		Valid: true,
	})
	if err != nil {
		t.Fatalf("listRuntimeUsage: %v", err)
	}
	byDate := make(map[string]int64)
	for _, row := range resp {
		if row.Provider == "cutoff-test" {
			byDate[row.Date] += row.InputTokens
		}
	}
	if byDate[cutoffDate] != 222 {
		t.Fatalf("expected cutoff date %s to be included with 222 tokens, got map %v", cutoffDate, byDate)
	}
	if byDate[extraDate] != 0 {
		t.Fatalf("expected extra date %s to be excluded, got map %v", extraDate, byDate)
	}
}

func TestUpdateAgentRuntimeTimezoneValidatesPermissionAndValue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var originalTZ string
	if err := testPool.QueryRow(ctx, `SELECT timezone FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&originalTZ); err != nil {
		t.Fatalf("read runtime timezone: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET timezone = $1 WHERE id = $2`, originalTZ, runtimeID)
		testPool.Exec(ctx, `DELETE FROM task_usage_daily WHERE runtime_id = $1 AND provider = 'patch-tz-test'`, runtimeID)
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]string{"timezone": "Asia/Shanghai"})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid timezone: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Timezone != "Asia/Shanghai" {
		t.Fatalf("expected timezone Asia/Shanghai, got %q", resp.Timezone)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]string{"timezone": "Mars/Olympus"})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid timezone: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var otherUserID string
	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = 'runtime-tz-member@multica.ai'`)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Runtime TZ Member', 'runtime-tz-member@multica.ai')
		RETURNING id
	`).Scan(&otherUserID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, otherUserID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]string{"timezone": "Asia/Tokyo"})
	req.Header.Set("X-User-ID", otherUserID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpsertAgentRuntimePreservesTimezoneOverride(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	testPool.Exec(ctx, `
		DELETE FROM agent_runtime
		 WHERE workspace_id = $1 AND daemon_id = 'tz-upsert-daemon' AND provider = 'tz-upsert-provider'
	`, testWorkspaceID)
	row, err := testHandler.Queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		DaemonID:    strToText("tz-upsert-daemon"),
		Name:        "Timezone Upsert Runtime",
		RuntimeMode: "local",
		Provider:    "tz-upsert-provider",
		Status:      "online",
		DeviceInfo:  "tz-upsert-device",
		Metadata:    []byte(`{}`),
		OwnerID:     parseUUID(testUserID),
		Timezone:    "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, row.ID)
	})

	updated, err := testHandler.Queries.UpdateAgentRuntimeTimezone(ctx, db.UpdateAgentRuntimeTimezoneParams{
		ID:       row.ID,
		Timezone: "America/New_York",
	})
	if err != nil {
		t.Fatalf("set override: %v", err)
	}
	if updated.Timezone != "America/New_York" {
		t.Fatalf("expected override to be set, got %q", updated.Timezone)
	}

	row, err = testHandler.Queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		DaemonID:    strToText("tz-upsert-daemon"),
		Name:        "Timezone Upsert Runtime",
		RuntimeMode: "local",
		Provider:    "tz-upsert-provider",
		Status:      "online",
		DeviceInfo:  "tz-upsert-device reconnect",
		Metadata:    []byte(`{}`),
		OwnerID:     pgtype.UUID{},
		Timezone:    "Asia/Tokyo",
	})
	if err != nil {
		t.Fatalf("reconnect upsert: %v", err)
	}
	if row.Timezone != "America/New_York" {
		t.Fatalf("daemon reconnect should preserve user override, got %q", row.Timezone)
	}
}
