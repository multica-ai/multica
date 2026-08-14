package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// dashboardFixtureTZ is the zone the day-boundary fixtures in this file are
// built in, and the zone their requests pin with `?tz=`. Both sides read this
// one constant because they have to agree: every `days=N` endpoint opens its
// window at start-of-day in the VIEWER's zone (resolveViewingTZ: `?tz=`, else
// the user's stored user.timezone, else UTC), so a fixture anchored in one
// zone and a window opened in another sit an offset apart. Without the
// `?tz=` these requests would inherit whatever user.timezone holds — NULL in
// the handler fixture, hence UTC.
//
// The zone is deliberately EAST of UTC, and that is what makes the pin
// load-bearing rather than decorative. A fixture built in UTC survives losing
// its `?tz=`: the fallback is UTC too, so the window opens where the fixture
// already sits and nothing notices. Built in Asia/Tokyo the run finishes at
// 15:10 UTC the previous day, so a request that falls back to UTC opens its
// window after the run ended and the assertions go red — which is the whole
// point of pinning it. Verified by mutation: dropping the parameter, and
// pinning it to UTC, both fail.
const dashboardFixtureTZ = "Asia/Tokyo"

// dashboardFixtureTZParam is dashboardFixtureTZ as a query fragment, so a
// request cannot pin a zone the fixture was not built in.
const dashboardFixtureTZParam = "tz=" + dashboardFixtureTZ

// dashboardFixtureLoc resolves dashboardFixtureTZ.
func dashboardFixtureLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(dashboardFixtureTZ)
	if err != nil {
		t.Fatalf("load fixture timezone %q: %v", dashboardFixtureTZ, err)
	}
	return loc
}

// runFinishedToday returns the started_at / completed_at of a ten-minute run
// placed in the first ten minutes of the day `now` falls in, read in loc.
//
// Every endpoint these fixtures exercise filters on `completed_at >= @since`
// — the END of the run, not its start — and for the per-agent rollups @since
// is start of today in the viewer's zone (parseExactSinceParamInTZ at
// days=1). Timestamps built as `now - 30m` therefore fall out of that window
// for the first twenty minutes after midnight: the run finished yesterday,
// the window opens today, and the assertions read the empty result as nothing
// having happened. A handler job that ran at 00:13 UTC failed on exactly
// that; the next one at 00:25 passed with nothing changed but the clock. The
// date-bucketed halves ride out those twenty minutes on the extra day
// parseSinceParamInTZ hands them, which is why only their per-agent siblings
// went red. Anchoring the run on the boundary itself takes the time of day
// out of the fixture rather than special-casing the twenty minutes it bites.
//
// Start-of-day is built the way sinceFromDays builds the cutoff, so fixture
// and window agree instant for instant. In the first ten minutes after
// midnight the run ends slightly in the future, which none of these queries
// mind: they bound completed_at from below only, and the token rollup keys
// off task_usage.created_at rather than the queue row.
func runFinishedToday(now time.Time, loc *time.Location) (started, completed time.Time) {
	local := now.In(loc)
	started = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return started, started.Add(10 * time.Minute)
}

// pinDayWindowClock freezes the clock the request-side cutoff reads and hands
// back the same instant for the fixture, so both sides describe one moment.
//
// They read the wall clock at two different points otherwise — the fixture when
// it is built, the cutoff when the handler runs, with inserts and a rollup in
// between — and a suite that crosses midnight in that gap writes its run into
// one day and then asks for the next day's window. The gap is milliseconds, so
// it is rare and permanent: nothing about the assertions says which day they
// meant.
func pinDayWindowClock(t *testing.T, at time.Time) time.Time {
	t.Helper()
	prev := dayWindowNow
	dayWindowNow = func() time.Time { return at }
	t.Cleanup(func() { dayWindowNow = prev })
	return at
}

// TestDashboardFixtureRunLandsInsideTheWindow pins what runFinishedToday
// promises the two DB fixtures that call it: at any hour, in any zone, the
// run it places is inside the days=1 window the endpoints open. The window is
// taken from sinceFromDays — the production cutoff itself — rather than from
// a second copy of the helper's arithmetic.
//
// The midnight rows are the point of the table. A fixture built off the wall
// clock is outside that window for the first twenty minutes of the day and
// inside it for the other 23h40m, so only a synthetic clock can hold it to
// account: the suite would otherwise have to run at midnight to see the
// failure, which is how this reached CI in the first place.
func TestDashboardFixtureRunLandsInsideTheWindow(t *testing.T) {
	// Half-hour and negative offsets, so a helper that anchored on UTC
	// midnight while the request asked for another zone cannot pass.
	for _, tz := range []string{dashboardFixtureTZ, "Asia/Kolkata", "America/Los_Angeles"} {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatalf("load %s: %v", tz, err)
		}
		for _, clock := range []string{
			"2026-03-01 00:00:00",
			"2026-03-01 00:05:00",
			"2026-03-01 00:19:59",
			"2026-03-01 12:00:00",
			"2026-03-01 23:59:59",
		} {
			t.Run(tz+" "+clock, func(t *testing.T) {
				now, err := time.ParseInLocation("2006-01-02 15:04:05", clock, loc)
				if err != nil {
					t.Fatalf("parse %s: %v", clock, err)
				}
				started, completed := runFinishedToday(now, loc)

				// parseExactSinceParamInTZ trims a day off sinceFromDays, so
				// the days=1 cutoff these endpoints use is start of today.
				since := sinceFromDays(now, 0, loc)
				tomorrow := since.AddDate(0, 0, 1)

				if got := completed.Sub(started); got != 10*time.Minute {
					t.Errorf("run lasted %s, want 10m — the >=600s the run-time assertion reads", got)
				}
				if completed.Before(since) {
					t.Errorf("completed_at %s precedes the days=1 cutoff %s: `completed_at >= @since` drops the fixture", completed, since)
				}
				if !completed.Before(tomorrow) {
					t.Errorf("completed_at %s is past the day that opened at %s", completed, since)
				}
				if started.Before(since) {
					t.Errorf("started_at %s precedes the days=1 cutoff %s", started, since)
				}
			})
		}
	}
}

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

	// A 600s run that finished inside today's window at every hour of the
	// clock — see runFinishedToday for what a `now - 30m` fixture does to the
	// agent-runtime assertions just after midnight.
	started, completed := runFinishedToday(pinDayWindowClock(t, time.Now()), dashboardFixtureLoc(t))

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
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1&"+dashboardFixtureTZParam, nil))
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
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1&"+dashboardFixtureTZParam+"&project_id="+projectID, nil))
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
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1&"+dashboardFixtureTZParam+"&project_id="+projectID, nil))
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
		testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?days=1&"+dashboardFixtureTZParam+"&project_id="+projectID, nil))
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
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1&"+dashboardFixtureTZParam, nil))
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

// TestDashboardRunTimeCountsCancelledRuns pins the fix for the run-time
// rollups dropping every run the user stopped mid-flight. CancelAgentTask
// accepts a 'running' task, so a cancelled row can carry both started_at and
// completed_at — real agent occupancy, and real tokens the cost rollup
// charges for regardless of status. The old `status IN ('completed','failed')`
// filter zeroed that time, so Time/Tasks and Cost/Tokens summed different
// task populations on the same dashboard.
//
// Also asserts the other half of the contract: a run cancelled while still
// queued (started_at NULL) must stay out, since it never occupied an agent.
func TestDashboardRunTimeCountsCancelledRuns(t *testing.T) {
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
		VALUES ($1, 'run-time cancelled test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Baseline before inserting, so a shared fixture DB with pre-existing
	// rows can't make the deltas below pass or fail spuriously.
	baseSeconds, baseTasks, baseCancelled := readAgentRunTime(t, agentID)

	// Stopped 15 minutes into the run: started_at and completed_at both set.
	var cancelledTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES ($1, $2, $3, 'cancelled', now() - interval '15 minutes', now(), now())
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&cancelledTaskID); err != nil {
		t.Fatalf("insert cancelled task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, cancelledTaskID) })

	// Cancelled from the queue: never started, so it must not contribute.
	var queuedCancelID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES ($1, $2, $3, 'cancelled', NULL, now(), now())
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&queuedCancelID); err != nil {
		t.Fatalf("insert queue-cancelled task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, queuedCancelID) })

	gotSeconds, gotTasks, gotCancelled := readAgentRunTime(t, agentID)

	// 15 minutes of occupancy, from exactly one of the two rows.
	if delta := gotSeconds - baseSeconds; delta < 890 || delta > 910 {
		t.Errorf("total_seconds delta = %d, want ~900 (15m from the stopped run only)", delta)
	}
	if delta := gotTasks - baseTasks; delta != 1 {
		t.Errorf("task_count delta = %d, want 1 (the queue-cancelled run must not count)", delta)
	}
	if delta := gotCancelled - baseCancelled; delta != 1 {
		t.Errorf("cancelled_count delta = %d, want 1", delta)
	}
}

// readAgentRunTime returns (total_seconds, task_count, cancelled_count) for
// one agent from GetDashboardAgentRunTime. Zeroes when the agent has no row.
func readAgentRunTime(t *testing.T, agentID string) (int64, int32, int32) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?days=10&tz=UTC", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []struct {
		AgentID        string `json:"agent_id"`
		TotalSeconds   int64  `json:"total_seconds"`
		TaskCount      int32  `json:"task_count"`
		CancelledCount int32  `json:"cancelled_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Fatalf("decode agent run time: %v", err)
	}
	for _, r := range rows {
		if r.AgentID == agentID {
			return r.TotalSeconds, r.TaskCount, r.CancelledCount
		}
	}
	return 0, 0, 0
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

// TestDashboardFailuresCountNeverStartedTasks pins the reason the failure
// rollups exist as their own queries rather than reusing the run-time ones:
// ListDashboardRunTimeDaily / ListDashboardAgentRunTime require
// `started_at IS NOT NULL`, so a task that expired in the queue — the exact
// signature of a runtime outage — contributes nothing to their failed_count.
// The failure endpoints must count it, and must report the succeeded tasks in
// the same payload so the client's error rate has a matching denominator.
func TestDashboardFailuresCountNeverStartedTasks(t *testing.T) {
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
		VALUES ($1, 'failures rollup test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Same window-safe fixture as TestDashboardEndpoints: the by-agent case
	// below runs on the exact start-of-day cutoff and filters on
	// completed_at, so a run timed off the wall clock disappears from it for
	// the first twenty minutes of the day.
	started, completed := runFinishedToday(pinDayWindowClock(t, time.Now()), dashboardFixtureLoc(t))

	// startedAt is nullable so the queue-expiry case can be modelled exactly:
	// completed_at set, started_at absent.
	mkTask := func(status string, failureReason any, startedAt any) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, failure_reason, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			RETURNING id
		`, agentID, issueID, runtimeID, status, startedAt, completed, failureReason).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}

	mkTask("completed", nil, started)
	mkTask("failed", "agent_error.provider_auth_or_access", started)
	mkTask("failed", "queued_expired", nil) // never started
	mkTask("failed", nil, started)          // unclassified: empty reason column

	type failureRow struct {
		Date          string `json:"date"`
		AgentID       string `json:"agent_id"`
		FailureReason string `json:"failure_reason"`
		TaskCount     int32  `json:"task_count"`
	}

	// The fixture workspace is shared, so other tests' rows may be in the
	// window too. Assert on the buckets this test wrote rather than on the
	// whole payload.
	collect := func(rows []failureRow) map[string]int32 {
		byReason := map[string]int32{}
		for _, r := range rows {
			byReason[r.FailureReason] += r.TaskCount
		}
		return byReason
	}

	for _, tc := range []struct {
		name string
		call func(w *httptest.ResponseRecorder)
	}{
		{"daily", func(w *httptest.ResponseRecorder) {
			testHandler.GetDashboardFailuresDaily(w, newRequest("GET", "/api/dashboard/failures/daily?days=1&"+dashboardFixtureTZParam, nil))
		}},
		{"by-agent", func(w *httptest.ResponseRecorder) {
			testHandler.GetDashboardFailuresByAgent(w, newRequest("GET", "/api/dashboard/failures/by-agent?days=1&"+dashboardFixtureTZParam, nil))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.call(w)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var rows []failureRow
			if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
				t.Fatalf("decode: %v", err)
			}
			byReason := collect(rows)

			if byReason["agent_error.provider_auth_or_access"] < 1 {
				t.Errorf("expected the classified failure to be counted, got %v", byReason)
			}
			// The point of the whole endpoint: a failure with no started_at
			// still lands in a bucket.
			if byReason["queued_expired"] < 1 {
				t.Errorf("expected the never-started failure to be counted, got %v", byReason)
			}
			// A failed row with an empty failure_reason must not be mistaken
			// for a success — that would deflate the error rate.
			if byReason["unclassified"] < 1 {
				t.Errorf("expected the reason-less failure to be counted as unclassified, got %v", byReason)
			}
			// Succeeded tasks ride along under the empty-string key so the
			// client can compute a rate from one payload.
			if byReason[""] < 1 {
				t.Errorf("expected succeeded tasks in the denominator bucket, got %v", byReason)
			}
		})
	}
}

// TestDashboardFailuresByAgentUsesExactWindow pins the cutoff difference
// between the two failure endpoints.
//
// parseSinceParamInTZ deliberately returns N+1 calendar days of headroom, and
// the workspace dashboard trims the surplus client-side with `-(days-1)`. The
// by-agent rollup carries no date column, so it cannot be trimmed that way —
// it must close its own window server-side. Before that fix, `days=1` served
// the Errors card yesterday's failures while the chart beside it, correctly
// trimmed, showed none.
func TestDashboardFailuresByAgentUsesExactWindow(t *testing.T) {
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
		VALUES ($1, 'failures window test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// One failure at noon YESTERDAY (UTC). days=1 means "today", so neither
	// endpoint may count it. Noon avoids the midnight edge in either
	// direction.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, failure_reason, created_at)
		VALUES (
			$1, $2, $3, 'failed',
			((CURRENT_DATE - 1)::timestamp + interval '11 hours') AT TIME ZONE 'UTC',
			((CURRENT_DATE - 1)::timestamp + interval '12 hours') AT TIME ZONE 'UTC',
			'timeout', now()
		)
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	type failureRow struct {
		FailureReason string `json:"failure_reason"`
		TaskCount     int32  `json:"task_count"`
	}
	countTimeouts := func(body []byte) int32 {
		var rows []failureRow
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var n int32
		for _, r := range rows {
			if r.FailureReason == "timeout" {
				n += r.TaskCount
			}
		}
		return n
	}

	w := httptest.NewRecorder()
	testHandler.GetDashboardFailuresByAgent(w, newRequest("GET", "/api/dashboard/failures/by-agent?days=1&tz=UTC", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("by-agent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := countTimeouts(w.Body.Bytes()); got != 0 {
		t.Errorf("days=1 must not reach yesterday's failure, but by-agent counted %d", got)
	}

	// days=2 covers today + yesterday, so the same row must now appear —
	// proving the window was closed, not that the fixture is unreachable.
	w = httptest.NewRecorder()
	testHandler.GetDashboardFailuresByAgent(w, newRequest("GET", "/api/dashboard/failures/by-agent?days=2&tz=UTC", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("by-agent days=2: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := countTimeouts(w.Body.Bytes()); got < 1 {
		t.Errorf("days=2 must include yesterday's failure, got %d", got)
	}
}

// TestDashboardPerAgentRollupsUseExactWindow is MUL-5551: the Usage page's
// leaderboard reported MORE tokens for a single agent than the Tokens KPI
// reported for the entire workspace.
//
// Both halves read the same underlying rows; they disagreed only on the
// window. usage/daily and runtime/daily are date-bucketed, so the client
// trims `parseSinceParamInTZ`'s extra calendar day back to `-(days-1)` before
// computing the KPIs and the chart. usage/by-agent and agent-runtime carry no
// date, so nothing trimmed them and they kept the whole N+1 span — at days=1
// that is today PLUS yesterday. One busy agent's two-day total then trivially
// exceeded the workspace's one-day total.
//
// Sibling of TestDashboardFailuresByAgentUsesExactWindow, which pinned the
// same cutoff for the third date-less rollup. All three now close their own
// window server-side.
func TestDashboardPerAgentRollupsUseExactWindow(t *testing.T) {
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
		VALUES ($1, 'per-agent window test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// A token bucket at noon YESTERDAY, under a provider/model pair no other
	// fixture uses so the assertion can't be satisfied by ambient rows.
	// Seeded straight into task_usage_hourly (same shortcut as the tz-bucket
	// test) — the rollup's own source column is task_usage.created_at, which
	// is `now()`-defaulted and awkward to backdate.
	const windowProvider = "exact-window-test"
	const windowModel = "exact-window-model"
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE runtime_id = $1 AND provider = $2`, runtimeID, windowProvider)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage_hourly (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id,
			provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, event_count
		)
		VALUES (
			((CURRENT_DATE - 1)::timestamp + interval '12 hours') AT TIME ZONE 'UTC',
			$1, $2, $3, NULL, $4, $5,
			7777, 0, 0, 0, 1
		)
		ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
			SET input_tokens = EXCLUDED.input_tokens
	`, testWorkspaceID, runtimeID, agentID, windowProvider, windowModel); err != nil {
		t.Fatalf("seed hourly row: %v", err)
	}

	// A terminal task that ran for 900s and completed at noon yesterday, for
	// the agent-runtime half. Noon keeps both fixtures clear of the midnight
	// edge in either direction.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES (
			$1, $2, $3, 'completed',
			((CURRENT_DATE - 1)::timestamp + interval '11 hours 45 minutes') AT TIME ZONE 'UTC',
			((CURRENT_DATE - 1)::timestamp + interval '12 hours') AT TIME ZONE 'UTC',
			now()
		)
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	seededTokens := func(body []byte) int64 {
		var rows []struct {
			AgentID     string `json:"agent_id"`
			Provider    string `json:"provider"`
			Model       string `json:"model"`
			InputTokens int64  `json:"input_tokens"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode by-agent: %v", err)
		}
		var n int64
		for _, r := range rows {
			if r.Model == windowModel {
				n += r.InputTokens
			}
		}
		return n
	}
	agentSeconds := func(body []byte) int64 {
		var rows []struct {
			AgentID      string `json:"agent_id"`
			TotalSeconds int64  `json:"total_seconds"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode agent-runtime: %v", err)
		}
		var n int64
		for _, r := range rows {
			if r.AgentID == agentID {
				n += r.TotalSeconds
			}
		}
		return n
	}

	get := func(path string) []byte {
		w := httptest.NewRecorder()
		switch {
		case strings.HasPrefix(path, "/api/dashboard/usage/by-agent"):
			testHandler.GetDashboardUsageByAgent(w, newRequest("GET", path, nil))
		default:
			testHandler.GetDashboardAgentRunTime(w, newRequest("GET", path, nil))
		}
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}

	// days=1 means "today". Neither per-agent rollup may reach yesterday.
	if got := seededTokens(get("/api/dashboard/usage/by-agent?days=1&tz=UTC")); got != 0 {
		t.Errorf("days=1 must not reach yesterday's tokens, but by-agent counted %d", got)
	}
	oneDaySeconds := agentSeconds(get("/api/dashboard/agent-runtime?days=1&tz=UTC"))

	// days=2 covers today + yesterday, so both fixtures must now appear —
	// proving the window closed rather than the fixtures being unreachable.
	if got := seededTokens(get("/api/dashboard/usage/by-agent?days=2&tz=UTC")); got < 7777 {
		t.Errorf("days=2 must include yesterday's 7777 tokens, got %d", got)
	}
	twoDaySeconds := agentSeconds(get("/api/dashboard/agent-runtime?days=2&tz=UTC"))
	if twoDaySeconds-oneDaySeconds < 900 {
		t.Errorf(
			"days=1 leaked yesterday's 900s run: days=1 reported %ds, days=2 %ds (delta %d, want >=900)",
			oneDaySeconds, twoDaySeconds, twoDaySeconds-oneDaySeconds,
		)
	}
}

// TestDashboardFailureWireContractKeepsEmptyReason pins the success bucket's
// wire form. The client's zod schema defaults a missing `failure_reason` to
// "" — the succeeded bucket — which is only safe while the server always
// emits the field. Adding `omitempty` to the struct tag would strip it from
// exactly the success rows and silently turn every window into a 100% error
// rate, so that regression is caught here rather than in a dashboard.
func TestDashboardFailureWireContractKeepsEmptyReason(t *testing.T) {
	// Each case decodes into its OWN map. json.Unmarshal merges into a
	// non-nil map rather than resetting it, so sharing one across cases would
	// leave the first payload's failure_reason in place and let a later
	// omitempty regression pass unnoticed — the exact failure this test
	// exists to catch.
	for _, tc := range []struct {
		name string
		row  any
	}{
		{"daily", DashboardFailureDailyResponse{Date: "2026-05-19", TaskCount: 3}},
		{"by-agent", DashboardFailureByAgentResponse{AgentID: "a", TaskCount: 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.row)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			decoded := map[string]any{}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, present := decoded["failure_reason"]; !present {
				t.Errorf("succeeded rows must serialize an explicit empty failure_reason, got %s", body)
			}
		})
	}
}

// dashboardRunTimeSeconds inserts one finished run for the fixture agent and
// returns what agent-runtime reports for it under the given query string. The
// caller owns the clock: whatever pinDayWindowClock was given is what both the
// fixture and the handler's cutoff read.
func dashboardRunTimeSeconds(t *testing.T, at time.Time, loc *time.Location, query string) int64 {
	t.Helper()
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
		VALUES ($1, 'clock fixture', $2, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	started, completed := runFinishedToday(at, loc)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES ($1, $2, $3, 'completed', $4, $5, $4)
		RETURNING id
	`, agentID, issueID, runtimeID, started, completed).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("agent-runtime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []struct {
		AgentID      string `json:"agent_id"`
		TotalSeconds int64  `json:"total_seconds"`
	}
	_ = json.NewDecoder(w.Body).Decode(&rows)
	var seconds int64
	for _, r := range rows {
		if r.AgentID == agentID {
			seconds += r.TotalSeconds
		}
	}
	return seconds
}

// A fixture built a hair before midnight is still counted by a request handled
// a hair after it.
//
// The two sides used to read the wall clock at different moments — the fixture
// when it was built, the cutoff when the handler ran, with inserts and a rollup
// in between. Crossing midnight in that gap wrote the run into one day and then
// asked for the next day's window, and the row vanished. Pinning one instant is
// what removes the question; this asserts the removal at the instant where it
// used to bite, rather than trusting that the suite never runs at 23:59.
func TestDashboardFixtureSurvivesTheMidnightStraddle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	loc := dashboardFixtureLoc(t)
	// 23:59:59.9 in the fixture's own zone: one tenth of a second of the day
	// left when the fixture is built.
	local := time.Date(2026, 3, 1, 23, 59, 59, 900_000_000, loc)
	at := pinDayWindowClock(t, local)

	if got := dashboardRunTimeSeconds(t, at, loc, "days=1&"+dashboardFixtureTZParam); got < 600 {
		t.Errorf("agent-runtime reported %ds for a run built at %s, want >=600 — "+
			"the fixture and the cutoff have to describe the same day even when the clock is about to turn over",
			got, local.Format(time.RFC3339Nano))
	}
}

// The timezone pin is load-bearing, at an instant that proves it.
//
// A request that loses its `?tz=` falls back to the fixture user's stored zone
// — UTC — while the fixture stays in dashboardFixtureTZ, and the two windows
// then sit an offset apart. Whether that offset actually hides the run depends
// on the time of day, so asserting it against the wall clock proves nothing for
// most of the day. Pinned at 02:00 UTC on a fixed date, Tokyo's day began at
// 15:00 UTC the day before and UTC's began two hours ago: the run is behind the
// UTC cutoff and must not be counted.
func TestDashboardMismatchedRequestTimezoneHidesTheRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	loc := dashboardFixtureLoc(t)
	at := pinDayWindowClock(t, time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC))

	matched := dashboardRunTimeSeconds(t, at, loc, "days=1&"+dashboardFixtureTZParam)
	if matched < 600 {
		t.Fatalf("the matched-timezone request reported %ds, want >=600 — this test's premise is gone", matched)
	}
	if mismatched := dashboardRunTimeSeconds(t, at, loc, "days=1&tz=UTC"); mismatched != 0 {
		t.Errorf("a request pinned to UTC counted %ds of a run the fixture placed in %s — "+
			"the pin is meant to be the thing that keeps the two windows together, so a mismatch has to be visible here",
			mismatched, dashboardFixtureTZ)
	}
}
