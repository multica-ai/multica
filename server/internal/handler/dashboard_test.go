package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDashboardEndpoints covers the workspace-dashboard rollups:
//   - daily token usage with and without project filter
//   - per-agent token usage with and without project filter
//   - per-agent run time
//
// Asserts that (1) tasks belonging to a project show up under the workspace
// view, (2) the project filter excludes tasks tied to issues without a
// matching project_id, and (3) run-time aggregation accumulates the
// completed_at − started_at delta correctly.
func TestDashboardEndpoints(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	// Two issues: one bound to a project, one not.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'dashboard test project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	// issue.number is `UNIQUE (workspace_id, number)` (migration 020) and
	// defaults to 0. Two inserts into the same workspace would collide on the
	// default; allocate `MAX(number) + 1` per row to stay sequential and
	// avoid stepping on rows other tests have left behind in the shared
	// fixture workspace.
	mkIssue := func(withProject bool) string {
		var id string
		var pid any
		if withProject {
			pid = projectID
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
			VALUES (
				$1, 'dashboard test', $2, 'member', $3,
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
			)
			RETURNING id
		`, testWorkspaceID, testUserID, pid).Scan(&id); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}
	projectIssueID := mkIssue(true)
	otherIssueID := mkIssue(false)

	now := time.Now().UTC()
	started := now.Add(-30 * time.Minute)
	completed := started.Add(10 * time.Minute) // 600s run

	mkTaskWithUsage := func(issueID string, status string, tokens int64) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			RETURNING id
		`, agentID, issueID, runtimeID, status, started, completed).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', 'claude-3-5-sonnet', $2, 0, now())
		`, taskID, tokens); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}

	mkTaskWithUsage(projectIssueID, "completed", 1000)
	mkTaskWithUsage(otherIssueID, "completed", 500)

	// All dashboard endpoints now read from task_usage_hourly (post-RFC
	// Phase 3). Drive the underlying window function directly so the
	// freshly inserted fixture rows are aggregated before assertions —
	// in production the cron tick handles this with a 5-min lag.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup window: %v", err)
	}

	type dailyRow struct {
		Date        string `json:"date"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	type byAgentRow struct {
		AgentID     string `json:"agent_id"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	type runtimeRow struct {
		AgentID      string `json:"agent_id"`
		TotalSeconds int64  `json:"total_seconds"`
		TaskCount    int32  `json:"task_count"`
	}

	// daily — workspace-wide
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("daily ws: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []dailyRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		var total int64
		for _, r := range rows {
			if r.Model == "claude-3-5-sonnet" {
				total += r.InputTokens
			}
		}
		if total < 1500 {
			t.Errorf("daily ws: expected >=1500 tokens (1000+500), got %d", total)
		}
	}

	// daily — project-scoped
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("daily project: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []dailyRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		var total int64
		for _, r := range rows {
			if r.Model == "claude-3-5-sonnet" {
				total += r.InputTokens
			}
		}
		// Project filter must exclude the 500-token "other" issue. Token total
		// for this project must be >= 1000 (our task) and < 1500 (would only
		// reach 1500 if filter leaked).
		if total < 1000 {
			t.Errorf("daily project: expected >=1000 tokens, got %d", total)
		}
		if total >= 1500 {
			t.Errorf("daily project: filter leaked — expected <1500 tokens, got %d", total)
		}
	}

	// by-agent — project-scoped
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("by-agent project: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []byAgentRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		found := false
		for _, r := range rows {
			if r.AgentID == agentID && r.InputTokens >= 1000 {
				found = true
			}
		}
		if !found {
			t.Errorf("by-agent project: expected agent %s with >=1000 tokens; got %v", agentID, rows)
		}
	}

	// agent-runtime — project-scoped
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("agent-runtime: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []runtimeRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		var seconds int64
		var tasks int32
		for _, r := range rows {
			if r.AgentID == agentID {
				seconds += r.TotalSeconds
				tasks += r.TaskCount
			}
		}
		if tasks < 1 {
			t.Errorf("agent-runtime: expected >=1 task for agent, got %d", tasks)
		}
		if seconds < 600 {
			t.Errorf("agent-runtime: expected >=600s (one 10-minute run), got %d", seconds)
		}
	}

	// agent-runtime — invalid project_id rejected
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?project_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("agent-runtime: expected 400 for invalid uuid, got %d", w.Code)
		}
	}

	// Workspace-wide by-agent through the same rollup, just to confirm
	// the no-project-filter shape matches up.
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("by-agent ws: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var aRows []byAgentRow
		_ = json.NewDecoder(w.Body).Decode(&aRows)
		var aTotal int64
		for _, r := range aRows {
			if r.AgentID == agentID && r.Model == "claude-3-5-sonnet" {
				aTotal += r.InputTokens
			}
		}
		if aTotal < 1500 {
			t.Errorf("by-agent ws: expected >=1500 tokens across workspace, got %d", aTotal)
		}
	}
}

// TestDashboardUsageDailyBucketsByViewerTimezone proves the `?tz=` query
// param drives the calendar-day boundary: the same UTC instant lands under
// a different `date` for a UTC viewer vs an America/Los_Angeles viewer.
// This is the core promise of the timezone-architecture RFC.
func TestDashboardUsageDailyBucketsByViewerTimezone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE runtime_id = $1 AND provider = 'tz-bucket-test'`, runtimeID)
	})
	// One bucket at 04:00 UTC two days ago. 04:00 UTC is still the
	// previous evening in America/Los_Angeles (UTC-7/-8), so the UTC
	// viewer and the LA viewer must see this row under different dates.
	var bucketHour time.Time
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task_usage_hourly (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id,
			provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, event_count
		)
		VALUES (
			((CURRENT_DATE - 2)::timestamp + interval '4 hours') AT TIME ZONE 'UTC',
			$1, $2, $3, NULL, 'tz-bucket-test', 'tz-bucket-model',
			999, 0, 0, 0, 1
		)
		ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
			SET input_tokens = EXCLUDED.input_tokens
		RETURNING bucket_hour
	`, testWorkspaceID, runtimeID, agentID).Scan(&bucketHour); err != nil {
		t.Fatalf("seed hourly row: %v", err)
	}

	utcDate := bucketHour.UTC().Format("2006-01-02")
	laLoc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load LA location: %v", err)
	}
	laDate := bucketHour.In(laLoc).Format("2006-01-02")
	if utcDate == laDate {
		t.Fatalf("test setup: UTC and LA dates must differ, both %s", utcDate)
	}

	readDate := func(tz string) string {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=10&tz="+tz, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("tz=%s: expected 200, got %d: %s", tz, w.Code, w.Body.String())
		}
		var rows []struct {
			Date  string `json:"date"`
			Model string `json:"model"`
		}
		_ = json.NewDecoder(w.Body).Decode(&rows)
		for _, r := range rows {
			if r.Model == "tz-bucket-model" {
				return r.Date
			}
		}
		t.Fatalf("tz=%s: tz-bucket-model row not found in %v", tz, rows)
		return ""
	}

	if got := readDate("UTC"); got != utcDate {
		t.Errorf("UTC viewer: expected date %s, got %s", utcDate, got)
	}
	if got := readDate("America/Los_Angeles"); got != laDate {
		t.Errorf("LA viewer: expected date %s, got %s", laDate, got)
	}
}

// TestDashboardRunTimeDailyBucketsByViewerTimezone proves the `?tz=` query
// param drives the calendar-day boundary of the Time / Tasks dashboard tab:
// GetDashboardRunTimeDaily applies `@tz` to `completed_at AT TIME ZONE @tz`
// on agent_task_queue. A task completed at 04:00 UTC is still the previous
// evening in America/Los_Angeles (UTC-7/-8), so the LA viewer must see the
// row under the prior calendar date relative to a UTC viewer.
func TestDashboardRunTimeDailyBucketsByViewerTimezone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	// Issue tagged so we can clean up just this test's rows.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'runtime-daily tz test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// completed_at at 04:00 UTC two days ago — still the prior evening in LA.
	// started_at 10 minutes earlier so the run has a non-zero duration.
	var completedAt time.Time
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES (
			$1, $2, $3, 'completed',
			((CURRENT_DATE - 2)::timestamp + interval '3 hours 50 minutes') AT TIME ZONE 'UTC',
			((CURRENT_DATE - 2)::timestamp + interval '4 hours') AT TIME ZONE 'UTC',
			now()
		)
		RETURNING id, completed_at
	`, agentID, issueID, runtimeID).Scan(&taskID, &completedAt); err != nil {
		t.Fatalf("insert completed task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	utcDate := completedAt.UTC().Format("2006-01-02")
	laLoc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load LA location: %v", err)
	}
	laDate := completedAt.In(laLoc).Format("2006-01-02")
	if utcDate == laDate {
		t.Fatalf("test setup: UTC and LA dates must differ, both %s", utcDate)
	}

	readRow := func(tz string) (string, int64, int32) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardRunTimeDaily(w, newRequest("GET", "/api/dashboard/runtime/daily?days=10&tz="+tz, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("tz=%s: expected 200, got %d: %s", tz, w.Code, w.Body.String())
		}
		var rows []struct {
			Date         string `json:"date"`
			TotalSeconds int64  `json:"total_seconds"`
			TaskCount    int32  `json:"task_count"`
		}
		_ = json.NewDecoder(w.Body).Decode(&rows)
		want := utcDate
		if tz == "America/Los_Angeles" {
			want = laDate
		}
		for _, r := range rows {
			if r.Date == want {
				return r.Date, r.TotalSeconds, r.TaskCount
			}
		}
		t.Fatalf("tz=%s: no row on expected date %s in %v", tz, want, rows)
		return "", 0, 0
	}

	if date, secs, count := readRow("UTC"); date != utcDate || count < 1 || secs < 600 {
		t.Errorf("UTC viewer: got date=%s seconds=%d count=%d, want date=%s seconds>=600 count>=1",
			date, secs, count, utcDate)
	}
	if date, secs, count := readRow("America/Los_Angeles"); date != laDate || count < 1 || secs < 600 {
		t.Errorf("LA viewer: got date=%s seconds=%d count=%d, want date=%s seconds>=600 count>=1",
			date, secs, count, laDate)
	}
}

// TestRollupTaskUsageHourlyIdempotentAndWatermark covers two pipeline
// invariants the deleted runtime_rollup_test.go used to guard for the
// legacy daily rollup: (1) re-running the window function over the same
// range produces identical totals, and (2) the cron entry point advances
// the rollup-state watermark and clears last_error.
func TestRollupTaskUsageHourlyIdempotentAndWatermark(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Serialise against any other package's test that touches the shared
	// rollup singleton / advisory lock 4246 (MUL-3980).
	lockRollupSingleton(t)
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID, taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'rollup idempotency', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now() - interval '20 minutes') RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'rollup-idem-model', 3333, 0, now() - interval '20 minutes')
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	readTotal := func() int64 {
		var total int64
		if err := testPool.QueryRow(ctx, `
			SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
			WHERE runtime_id = $1 AND model = 'rollup-idem-model'
		`, runtimeID).Scan(&total); err != nil {
			t.Fatalf("read total: %v", err)
		}
		return total
	}

	// Idempotency: two passes over the same range must not double-count.
	for i := 0; i < 2; i++ {
		if _, err := testPool.Exec(ctx, `
			SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
		`); err != nil {
			t.Fatalf("rollup pass %d: %v", i, err)
		}
	}
	if got := readTotal(); got != 3333 {
		t.Errorf("idempotency: expected exactly 3333 tokens after two passes, got %d", got)
	}

	// Watermark advance: park the watermark an hour back, run the cron
	// entry, confirm it moved forward to ~now()-5min with no error.
	if _, err := testPool.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - interval '1 hour', last_error = 'stale'
		 WHERE id = 1
	`); err != nil {
		t.Fatalf("park watermark: %v", err)
	}
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_hourly()`); err != nil {
		t.Fatalf("rollup_task_usage_hourly: %v", err)
	}
	var watermarkAge time.Duration
	var lastError *string
	var ageSeconds float64
	if err := testPool.QueryRow(ctx, `
		SELECT EXTRACT(EPOCH FROM (now() - watermark_at)), last_error
		FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&ageSeconds, &lastError); err != nil {
		t.Fatalf("read rollup state: %v", err)
	}
	watermarkAge = time.Duration(ageSeconds) * time.Second
	// Watermark should sit at now()-5min (the cron upper bound), well
	// short of the 1-hour-back value we parked it at.
	if watermarkAge > 10*time.Minute {
		t.Errorf("watermark did not advance: still %s behind now()", watermarkAge)
	}
	if lastError != nil {
		t.Errorf("expected last_error cleared, got %q", *lastError)
	}
}

// TestRollupTaskUsageHourlyReassignBetweenRuntimes ports the invalidation
// coverage the deleted runtime_rollup_test.go held for the legacy daily
// rollup. Reassigning a task between runtimes (the runtime-merge path) must
// move its usage: the `trg_atq_dirty_hourly` trigger enqueues both the old
// and new runtime buckets, and the next window run drains the queue,
// empties the old bucket, and fills the new one. Without this the rollup
// keeps attributing usage to the merged-away runtime.
func TestRollupTaskUsageHourlyReassignBetweenRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	oldRuntimeID := handlerTestRuntimeID(t)
	var newRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'reassign-target-hourly', 'cloud', 'reassign-target-hourly', 'online', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID).Scan(&newRuntimeID); err != nil {
		t.Fatalf("create dest runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'reassign hourly test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	usageAt := time.Date(2021, 3, 14, 1, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', $4) RETURNING id
	`, agentID, issueID, oldRuntimeID, usageAt).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at, updated_at)
		VALUES ($1, 'claude', 'm-reassign-hourly', 700, 70, $2, $2)
	`, taskID, usageAt); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE model = 'm-reassign-hourly'`)
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly_dirty WHERE model = 'm-reassign-hourly'`)
	})

	runWindow := func(label string) {
		if _, err := testPool.Exec(ctx, `
			SELECT rollup_task_usage_hourly_window('-infinity'::timestamptz, 'infinity'::timestamptz)
		`); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	runtimeTotal := func(rt string) int64 {
		var total int64
		testPool.QueryRow(ctx, `
			SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
			WHERE runtime_id = $1 AND model = 'm-reassign-hourly'
		`, rt).Scan(&total)
		return total
	}

	runWindow("initial rollup")
	if old, new := runtimeTotal(oldRuntimeID), runtimeTotal(newRuntimeID); old != 700 || new != 0 {
		t.Fatalf("initial: expected old=700 new=0, got old=%d new=%d", old, new)
	}

	// Reassignment fires trg_atq_dirty_hourly, which enqueues the OLD and
	// NEW runtime buckets (same bucket_hour, two runtime_ids).
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET runtime_id = $1 WHERE id = $2`, newRuntimeID, taskID); err != nil {
		t.Fatalf("reassign task: %v", err)
	}
	var dirtyCount int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_hourly_dirty WHERE model = 'm-reassign-hourly'`).Scan(&dirtyCount)
	if dirtyCount != 2 {
		t.Fatalf("expected 2 dirty entries (old+new runtime), got %d", dirtyCount)
	}

	runWindow("rollup after reassign")
	if old, new := runtimeTotal(oldRuntimeID), runtimeTotal(newRuntimeID); old != 0 || new != 700 {
		t.Fatalf("after reassign: expected old=0 new=700, got old=%d new=%d", old, new)
	}
	// The window function must drain every queue row whose enqueued_at
	// predates p_to — a regression on that DELETE pins recomputes forever.
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_hourly_dirty WHERE model = 'm-reassign-hourly'`).Scan(&dirtyCount)
	if dirtyCount != 0 {
		t.Errorf("expected dirty queue drained, got %d entries", dirtyCount)
	}
}

// TestRollupTaskUsageHourlyWorkspaceMismatch constructs an atq row whose
// agent.workspace_id differs from issue.workspace_id and verifies the
// hourly rollup resolves workspace_id consistently from `agent` across the
// trigger, dirty_from_updates, and the recompute join. If any path leaked
// back to issue.workspace_id the dirty key would miss the recompute join
// and the bucket would be dropped or mis-attributed across tenants. The
// schema does not enforce the two workspace_ids match, so this canary
// keeps the alignment honest.
func TestRollupTaskUsageHourlyWorkspaceMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('ws-mismatch-hourly', 'ws-mismatch-hourly-' || gen_random_uuid()::text)
		RETURNING id
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID) })

	var foreignRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'mismatch-rt-hourly', 'cloud', 'mismatch-rt-hourly', 'online', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, foreignWorkspaceID).Scan(&foreignRuntimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}
	var foreignAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, 'mismatch-agent-hourly', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, foreignWorkspaceID, foreignRuntimeID, testUserID).Scan(&foreignAgentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}

	// Issue lives in the primary test workspace; the agent lives in the
	// foreign one — so agent.workspace_id != issue.workspace_id.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'mismatch hourly test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	usageAt := time.Date(2021, 9, 9, 1, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', $4) RETURNING id
	`, foreignAgentID, issueID, foreignRuntimeID, usageAt).Scan(&taskID); err != nil {
		t.Fatalf("insert atq: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at, updated_at)
		VALUES ($1, 'claude', 'm-mismatch-hourly', 333, 33, $2, $2)
	`, taskID, usageAt); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE model = 'm-mismatch-hourly'`)
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly_dirty WHERE model = 'm-mismatch-hourly'`)
	})

	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('-infinity'::timestamptz, 'infinity'::timestamptz)
	`); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	wsTotal := func(ws string) int64 {
		var total int64
		testPool.QueryRow(ctx, `
			SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
			WHERE workspace_id = $1 AND model = 'm-mismatch-hourly'
		`, ws).Scan(&total)
		return total
	}
	if got := wsTotal(foreignWorkspaceID); got != 333 {
		t.Fatalf("expected foreign workspace bucket = 333 (resolved from agent), got %d", got)
	}
	if got := wsTotal(testWorkspaceID); got != 0 {
		t.Errorf("expected primary workspace bucket = 0 (issue.workspace_id must not leak), got %d", got)
	}
}

// TestDashboardRollupReattributesOnProjectChange verifies the trigger that
// fires on `UPDATE issue SET project_id` enqueues both old + new project
// buckets so the next rollup tick re-attributes the affected tokens.
// Uses the rollup window function directly to drain the dirty queue,
// then asserts the rollup table reflects the new project_id.
func TestDashboardRollupReattributesOnProjectChange(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	mkProject := func(name string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
		`, testWorkspaceID, name).Scan(&id); err != nil {
			t.Fatalf("create project: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, id) })
		return id
	}
	projectA := mkProject("dashboard reattr A")
	projectB := mkProject("dashboard reattr B")

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES ($1, 'reattr issue', $2, 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, projectA).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now()) RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'claude-3-5-sonnet', 7777, 0, now())
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	// First rollup pass: tokens attributed to project A.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup A: %v", err)
	}
	var aTokens int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectA, agentID).Scan(&aTokens); err != nil {
		t.Fatalf("read A rollup: %v", err)
	}
	if aTokens < 7777 {
		t.Fatalf("project A: expected >=7777 tokens after first rollup, got %d", aTokens)
	}

	// Move the issue to project B. Trigger enqueues both A and B buckets.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectB, issueID); err != nil {
		t.Fatalf("reassign project: %v", err)
	}
	// Second rollup pass: A bucket drops to zero (deleted_empty), B
	// bucket gets the tokens.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup B: %v", err)
	}

	var bTokens, aTokensAfter int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectB, agentID).Scan(&bTokens); err != nil {
		t.Fatalf("read B rollup: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectA, agentID).Scan(&aTokensAfter); err != nil {
		t.Fatalf("read A rollup after move: %v", err)
	}
	if bTokens < 7777 {
		t.Errorf("project B: expected >=7777 tokens after reassign + rollup, got %d", bTokens)
	}
	if aTokensAfter != 0 {
		t.Errorf("project A: expected 0 tokens after reassign + rollup, got %d", aTokensAfter)
	}
}

// TestDashboardRollupClearsOnIssueDelete verifies that deleting an issue
// (which cascades to its tasks and task_usage rows) also clears the
// dashboard rollup row attributed to that issue's project. The
// `issue BEFORE DELETE` trigger has to fire ahead of the cascade so the
// dirty queue captures the original project_id while the issue row is
// still readable.
func TestDashboardRollupClearsOnIssueDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'dashboard cascade test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES ($1, 'cascade issue', $2, 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	// No t.Cleanup deleting the issue — that's what the test exercises.

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now()) RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	// Don't bother cleaning up taskID either; cascade will take it.

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'claude-3-5-sonnet', 4242, 0, now())
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	// First rollup: project bucket exists with 4242 tokens.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup before delete: %v", err)
	}
	var before int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id = $2
	`, testWorkspaceID, projectID).Scan(&before); err != nil {
		t.Fatalf("read before: %v", err)
	}
	if before < 4242 {
		t.Fatalf("project bucket: expected >=4242 tokens before delete, got %d", before)
	}

	// Delete the issue. Cascade removes atq + task_usage. The issue
	// BEFORE DELETE trigger should have enqueued the project bucket
	// before the cascade started.
	if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup after delete: %v", err)
	}
	var after int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id = $2
	`, testWorkspaceID, projectID).Scan(&after); err != nil {
		t.Fatalf("read after: %v", err)
	}
	if after != 0 {
		t.Errorf("project bucket: expected 0 tokens after issue delete, got %d", after)
	}
}

// TestDashboardRollupReattributesOnLinkTaskToIssue verifies that
// `LinkTaskToIssue` (which UPDATEs `agent_task_queue.issue_id` from NULL
// to a real issue id) re-attributes existing rollup rows from the
// no-project bucket to the linked issue's project bucket. Mirrors the
// quick-create flow in `service.task.LinkTaskToIssue`.
func TestDashboardRollupReattributesOnLinkTaskToIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	// Quick-create task: issue_id is NULL at creation time.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, context, created_at)
		VALUES ($1, NULL, $2, 'completed', '{}'::jsonb, now()) RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert quick-create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'claude-3-5-sonnet', 1234, 0, now())
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	// First rollup: tokens attributed to the no-project bucket (NULL).
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup pre-link: %v", err)
	}
	var nullBefore int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id IS NULL AND agent_id = $2
	`, testWorkspaceID, agentID).Scan(&nullBefore); err != nil {
		t.Fatalf("read NULL bucket pre-link: %v", err)
	}
	if nullBefore < 1234 {
		t.Fatalf("NULL bucket: expected >=1234 tokens pre-link, got %d", nullBefore)
	}

	// Create a project + issue, then run the same UPDATE LinkTaskToIssue
	// uses. The atq trigger should enqueue OLD (NULL project) AND NEW
	// (the project's id) so the next rollup tick zeroes the NULL bucket
	// and populates the project bucket.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'dashboard link test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES ($1, 'link test issue', $2, 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Mirror LinkTaskToIssue's UPDATE shape.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET issue_id = $1 WHERE id = $2 AND issue_id IS NULL
	`, issueID, taskID); err != nil {
		t.Fatalf("link task to issue: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup post-link: %v", err)
	}

	var projectAfter, nullAfter int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectID, agentID).Scan(&projectAfter); err != nil {
		t.Fatalf("read project bucket post-link: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
		WHERE workspace_id = $1 AND project_id IS NULL AND agent_id = $2
	`, testWorkspaceID, agentID).Scan(&nullAfter); err != nil {
		t.Fatalf("read NULL bucket post-link: %v", err)
	}
	if projectAfter < 1234 {
		t.Errorf("project bucket: expected >=1234 tokens after link, got %d", projectAfter)
	}
	if nullAfter != 0 {
		t.Errorf("NULL bucket: expected 0 tokens after link, got %d", nullAfter)
	}
}

// TestPruneTaskUsageHourlyDirty covers the dirty-queue TTL. Both the RFC
// (§7.1) and the rollup-pipeline migration call this THE most-easily-missed correctness
// requirement of the hourly pipeline: without the prune, a row that escapes
// the per-tick drain (crash mid-tick, worker paused during an incident)
// pins its bucket's recompute forever and the queue grows unbounded.
func TestPruneTaskUsageHourlyDirty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// This test calls rollup_task_usage_hourly(), which advances the shared
	// watermark as a side effect; serialise so it does not perturb another
	// package's rollup test (MUL-3980).
	lockRollupSingleton(t)
	ctx := context.Background()

	// task_usage_hourly_dirty carries no FKs (it is a queue), so synthetic
	// UUIDs are fine. `provider` tags the rows for isolated cleanup.
	const tag = "ttl-prune-test"
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly_dirty WHERE provider = $1`, tag)
	})
	seed := func(model, age string) {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage_hourly_dirty (
				bucket_hour, workspace_id, runtime_id, agent_id, project_id,
				provider, model, enqueued_at
			)
			VALUES (
				date_trunc('hour', now()), gen_random_uuid(), gen_random_uuid(),
				gen_random_uuid(), NULL, $1, $2, now() - $3::interval
			)
		`, tag, model, age); err != nil {
			t.Fatalf("seed dirty row %s: %v", model, err)
		}
	}
	countModel := func(model string) int {
		var n int
		testPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM task_usage_hourly_dirty WHERE provider = $1 AND model = $2`,
			tag, model,
		).Scan(&n)
		return n
	}

	seed("stale-row", "8 days")
	seed("fresh-row", "1 day")

	// Default 7-day retention: the 8-day row goes, the 1-day row stays.
	var pruned int64
	if err := testPool.QueryRow(ctx, `SELECT prune_task_usage_hourly_dirty()`).Scan(&pruned); err != nil {
		t.Fatalf("prune (default retention): %v", err)
	}
	if pruned < 1 {
		t.Errorf("expected prune to report at least the one stale row deleted, got %d", pruned)
	}
	if got := countModel("stale-row"); got != 0 {
		t.Errorf("default prune: expected stale row deleted, still %d present", got)
	}
	if got := countModel("fresh-row"); got != 1 {
		t.Errorf("default prune: expected fresh row kept, got %d", got)
	}

	// An explicit retention shorter than the surviving row's age drops it.
	if _, err := testPool.Exec(ctx, `SELECT prune_task_usage_hourly_dirty(interval '12 hours')`); err != nil {
		t.Fatalf("prune (12h retention): %v", err)
	}
	if got := countModel("fresh-row"); got != 0 {
		t.Errorf("12h-retention prune: expected fresh row deleted, still %d present", got)
	}

	// The cron entry folds the prune in so operators do
	// not need a second scheduled job. A single tick must drop a stale row.
	seed("cron-fold-row", "9 days")
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_hourly()`); err != nil {
		t.Fatalf("rollup_task_usage_hourly: %v", err)
	}
	if got := countModel("cron-fold-row"); got != 0 {
		t.Errorf("cron entry did not fold in the prune: stale row still present (%d)", got)
	}
}

// TestRollupTaskUsageHourlyCapsWindowAtOneDay covers the catch-up cap
// in rollup_task_usage_hourly(): when the watermark has
// fallen far behind (worker paused for an incident or a migration freeze),
// a single tick must advance it by at most one day, so a multi-week backlog
// drains in bounded steps instead of one giant statement holding advisory
// lock 4246. The existing watermark test only parks the watermark one hour
// back, so the cap itself is never exercised there.
func TestRollupTaskUsageHourlyCapsWindowAtOneDay(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Serialise against any other package's rollup test (MUL-3980). Acquire
	// the guard BEFORE registering the watermark-restore cleanup below so
	// that cleanup (LIFO) runs while the guard is still held, and the guard
	// is released last.
	lockRollupSingleton(t)
	ctx := context.Background()

	// Other tests drive rollup_task_usage_hourly_window directly and never
	// read the watermark; the idempotency test parks it itself. Restore to
	// now() so nothing downstream observes a stale value.
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`UPDATE task_usage_hourly_rollup_state SET watermark_at = now(), last_error = NULL WHERE id = 1`)
	})

	park := func(behind string) {
		if _, err := testPool.Exec(ctx, `
			UPDATE task_usage_hourly_rollup_state
			   SET watermark_at = now() - $1::interval, last_error = NULL
			 WHERE id = 1
		`, behind); err != nil {
			t.Fatalf("park watermark: %v", err)
		}
	}
	ageDays := func() float64 {
		var sec float64
		if err := testPool.QueryRow(ctx, `
			SELECT EXTRACT(EPOCH FROM (now() - watermark_at))
			  FROM task_usage_hourly_rollup_state WHERE id = 1
		`).Scan(&sec); err != nil {
			t.Fatalf("read watermark: %v", err)
		}
		return sec / 86400
	}
	tick := func(label string) {
		if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_hourly()`); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	// Park 3 days back. One tick advances by exactly one day (v_from + 1d,
	// well short of now()-5min), leaving the watermark ~2 days behind.
	park("3 days")
	tick("tick 1")
	if age := ageDays(); age < 1.9 || age > 2.1 {
		t.Fatalf("after one tick: expected watermark ~2 days behind, got %.3f days", age)
	}

	// A second tick advances another bounded day → ~1 day behind.
	tick("tick 2")
	if age := ageDays(); age < 0.9 || age > 1.1 {
		t.Fatalf("after two ticks: expected watermark ~1 day behind, got %.3f days", age)
	}

	// Once within a day of now, the tick snaps the watermark to now()-5min
	// (LEAST picks the now bound) rather than taking a further fixed day.
	tick("tick 3")
	if age := ageDays(); age > 0.02 {
		t.Fatalf("after catch-up: expected watermark within minutes of now, got %.3f days", age)
	}
}

// TestDashboardUsageDailyCrossMidnightFullPipeline runs the WHOLE timezone
// pipeline end to end: insert a raw `task_usage` row near UTC midnight →
// run `rollup_task_usage_hourly_window` to bucket it → call
// GetDashboardUsageDaily with a non-UTC viewer tz. It asserts the tokens
// land on the viewer's correct calendar day and NOT on the UTC day.
//
// This is the #2822 bug class the RFC exists to prevent. The existing
// TestDashboardUsageDailyBucketsByViewerTimezone seeds a pre-built
// task_usage_hourly row and only exercises the SQL read path; here the
// row travels from raw task_usage through the rollup, so a regression in
// task_usage_hour_bucket or the recompute join is also caught.
func TestDashboardUsageDailyCrossMidnightFullPipeline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'cross-midnight pipeline test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now()) RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// Raw task_usage at 00:30 UTC two days ago — genuinely near UTC
	// midnight. 00:30 UTC is still the PRIOR evening (~16:30/17:30) in
	// America/Los_Angeles (UTC-7/-8), so the UTC viewer and the LA viewer
	// must see this row under different calendar days. Using CURRENT_DATE
	// keeps the row inside the days=10 window without a fixed-date drift.
	var usageAt time.Time
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES (
			$1, 'claude', 'cross-midnight-model', 8888, 0,
			((CURRENT_DATE - 2)::timestamp + interval '30 minutes') AT TIME ZONE 'UTC'
		)
		RETURNING created_at
	`, taskID).Scan(&usageAt); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE model = 'cross-midnight-model'`)
	})

	// Run the rollup so the raw row is aggregated into task_usage_hourly.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup window: %v", err)
	}

	utcDate := usageAt.UTC().Format("2006-01-02")
	laLoc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load LA location: %v", err)
	}
	laDate := usageAt.In(laLoc).Format("2006-01-02")
	if utcDate == laDate {
		t.Fatalf("test setup: UTC and LA dates must differ, both %s", utcDate)
	}

	readDate := func(tz string) string {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=10&tz="+tz, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("tz=%s: expected 200, got %d: %s", tz, w.Code, w.Body.String())
		}
		var rows []struct {
			Date        string `json:"date"`
			Model       string `json:"model"`
			InputTokens int64  `json:"input_tokens"`
		}
		_ = json.NewDecoder(w.Body).Decode(&rows)
		for _, r := range rows {
			if r.Model == "cross-midnight-model" {
				if r.InputTokens != 8888 {
					t.Errorf("tz=%s: expected 8888 tokens, got %d", tz, r.InputTokens)
				}
				return r.Date
			}
		}
		t.Fatalf("tz=%s: cross-midnight-model row not found in %v", tz, rows)
		return ""
	}

	if got := readDate("UTC"); got != utcDate {
		t.Errorf("UTC viewer: expected date %s, got %s", utcDate, got)
	}
	if got := readDate("America/Los_Angeles"); got != laDate {
		t.Errorf("LA viewer: expected date %s, got %s; row must NOT land on the UTC day %s",
			laDate, got, utcDate)
	}
}

// TestRollupTaskUsageHourlyConvergesOnTaskUsageDelete covers the
// `trg_tu_dirty_hourly` trigger — a BEFORE DELETE trigger on task_usage.
// Migration 102 notes it has no production callers today and exists purely
// as defensive convergence guard, so a single minimal test is enough:
// seed a task_usage row, roll it up, DELETE the task_usage row directly,
// roll up again, and assert the hourly bucket is recomputed down to zero.
// Without the trigger the deleted row's bucket would never be re-enqueued.
func TestRollupTaskUsageHourlyConvergesOnTaskUsageDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'tu-delete trigger test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now() - interval '30 minutes') RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	var usageID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'tu-delete-model', 5050, 0, now() - interval '30 minutes')
		RETURNING id
	`, taskID).Scan(&usageID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE model = 'tu-delete-model'`)
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly_dirty WHERE model = 'tu-delete-model'`)
	})

	bucketTotal := func() int64 {
		var total int64
		testPool.QueryRow(ctx, `
			SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_hourly
			WHERE runtime_id = $1 AND model = 'tu-delete-model'
		`, runtimeID).Scan(&total)
		return total
	}
	runWindow := func(label string) {
		if _, err := testPool.Exec(ctx, `
			SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
		`); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	runWindow("initial rollup")
	if got := bucketTotal(); got != 5050 {
		t.Fatalf("initial: expected bucket = 5050, got %d", got)
	}

	// Delete the task_usage row directly — fires trg_tu_dirty_hourly,
	// which enqueues the bucket on task_usage_hourly_dirty.
	if _, err := testPool.Exec(ctx, `DELETE FROM task_usage WHERE id = $1`, usageID); err != nil {
		t.Fatalf("delete task_usage: %v", err)
	}
	var dirtyCount int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_hourly_dirty WHERE model = 'tu-delete-model'`).Scan(&dirtyCount)
	if dirtyCount != 1 {
		t.Fatalf("expected 1 dirty entry from task_usage DELETE trigger, got %d", dirtyCount)
	}

	runWindow("rollup after delete")
	if got := bucketTotal(); got != 0 {
		t.Errorf("after delete: expected bucket recomputed to 0, got %d", got)
	}
}
