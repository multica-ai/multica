package handler

import (
	"context"
	"testing"
	"time"
)

// TestRollupTaskUsageDaily_AggregatesAndIsIdempotent exercises the
// rollup_task_usage_daily_window() SQL function directly. This is the
// shared aggregation primitive used by both the cron-driven watermark
// loop and the offline backfill command, so its correctness underpins
// the entire ListRuntimeUsage read path. Two properties matter:
//
//  1. It correctly groups raw `task_usage` rows by (date, runtime,
//     workspace, provider, model) and sums the four token columns.
//  2. Re-aggregating an already-rolled-up window is *idempotent*: the
//     function recomputes each dirty bucket from ground truth and
//     REPLACES the daily row, so overlap with backfill / replay is
//     safe and corrections via UpsertTaskUsage propagate cleanly.
func TestRollupTaskUsageDaily_AggregatesAndIsIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := handlerTestRuntimeID(t)
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'rollup test', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Pin the test to a fixed historical day so we don't collide with
	// concurrent rollups of "today" running against the same fixture
	// runtime. 2020-06-15 is far outside any backfill window the rest
	// of the suite touches.
	day := time.Date(2020, 6, 15, 0, 0, 0, 0, time.UTC)

	// Two rows on the same (date, provider, model) — must collapse to
	// a single output row whose totals sum the inputs.
	insertUsage := func(usageAt time.Time, model string, in, out int64) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
			VALUES ($1, $2, $3, 'completed', $4)
			RETURNING id
		`, agentID, issueID, runtimeID, usageAt).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at, updated_at)
			VALUES ($1, 'claude', $2, $3, $4, $5, $5)
		`, taskID, model, in, out, usageAt); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
	}

	insertUsage(day.Add(1*time.Hour), "claude-3-5-sonnet", 100, 10)
	insertUsage(day.Add(2*time.Hour), "claude-3-5-sonnet", 200, 20)
	// A second model on the same day must produce a *separate* output
	// row (different group key).
	insertUsage(day.Add(3*time.Hour), "claude-3-5-haiku", 50, 5)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date`, runtimeID, day)
	})

	// --- 1) Initial aggregation produces the expected totals.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_daily_window($1::timestamptz, $2::timestamptz)
	`, day, day.Add(24*time.Hour)); err != nil {
		t.Fatalf("rollup_task_usage_daily_window: %v", err)
	}

	type row struct {
		Model       string
		InputTokens int64
		Output      int64
		EventCount  int64
	}
	read := func() map[string]row {
		rs, err := testPool.Query(ctx, `
			SELECT model, input_tokens, output_tokens, event_count
			  FROM task_usage_daily
			 WHERE runtime_id = $1 AND bucket_date = $2::date
		`, runtimeID, day)
		if err != nil {
			t.Fatalf("read task_usage_daily: %v", err)
		}
		defer rs.Close()
		out := map[string]row{}
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.Model, &r.InputTokens, &r.Output, &r.EventCount); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out[r.Model] = r
		}
		return out
	}

	got := read()
	if len(got) != 2 {
		t.Fatalf("expected 2 rows (one per model), got %d: %+v", len(got), got)
	}
	if got["claude-3-5-sonnet"].InputTokens != 300 || got["claude-3-5-sonnet"].Output != 30 || got["claude-3-5-sonnet"].EventCount != 2 {
		t.Errorf("sonnet bucket wrong: %+v", got["claude-3-5-sonnet"])
	}
	if got["claude-3-5-haiku"].InputTokens != 50 || got["claude-3-5-haiku"].Output != 5 || got["claude-3-5-haiku"].EventCount != 1 {
		t.Errorf("haiku bucket wrong: %+v", got["claude-3-5-haiku"])
	}

	// --- 2) Re-aggregating the same window is idempotent.
	// The new function recomputes each dirty bucket from ground truth and
	// REPLACES the daily row, so callers can safely overlap windows
	// (cron + backfill, replay, manual ops). Verifying it explicitly so
	// the property doesn't silently regress.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_daily_window($1::timestamptz, $2::timestamptz)
	`, day, day.Add(24*time.Hour)); err != nil {
		t.Fatalf("rollup_task_usage_daily_window (second call): %v", err)
	}
	got = read()
	if got["claude-3-5-sonnet"].InputTokens != 300 || got["claude-3-5-sonnet"].EventCount != 2 {
		t.Errorf("after second call, sonnet should be unchanged (idempotent), got: %+v", got["claude-3-5-sonnet"])
	}
	if got["claude-3-5-haiku"].InputTokens != 50 || got["claude-3-5-haiku"].EventCount != 1 {
		t.Errorf("after second call, haiku should be unchanged (idempotent), got: %+v", got["claude-3-5-haiku"])
	}

	// --- 3) Correction propagates: bumping a row's updated_at into a
	// new window must cause the bucket to be recomputed from ground
	// truth (covers the UpsertTaskUsage correction path that the old
	// additive design dropped silently).
	correctionMark := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := testPool.Exec(ctx, `
		UPDATE task_usage SET input_tokens = 1000, updated_at = $1
		 WHERE task_id IN (
		   SELECT id FROM agent_task_queue WHERE runtime_id = $2 AND created_at::date = $3::date
		 )
		   AND model = 'claude-3-5-sonnet'
		   AND input_tokens = 100
	`, correctionMark, runtimeID, day); err != nil {
		t.Fatalf("simulate correction: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_daily_window($1::timestamptz, $2::timestamptz)
	`, correctionMark.Add(-time.Minute), correctionMark.Add(time.Minute)); err != nil {
		t.Fatalf("rollup correction window: %v", err)
	}
	got = read()
	// New sonnet total: 1000 + 200 = 1200, still 2 events.
	if got["claude-3-5-sonnet"].InputTokens != 1200 || got["claude-3-5-sonnet"].EventCount != 2 {
		t.Errorf("after correction, sonnet should reflect new total 1200, got: %+v", got["claude-3-5-sonnet"])
	}
}

// TestRollupTaskUsageDaily_WatermarkAdvances verifies the cron entry
// point: rollup_task_usage_daily() consults task_usage_rollup_state to
// decide its window, performs the upsert, and bumps the watermark.
// We seed the watermark to a known value, force time to pass via a
// fixture, and assert the watermark moves forward by exactly the
// elapsed-window minus the 5 minute safety lag built into the function.
func TestRollupTaskUsageDaily_WatermarkAdvances(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Seed the watermark to "long ago" so the next call has a non-empty
	// window. Use a test-scoped low value so we don't clobber any other
	// test's state — the singleton row gets restored at the end.
	var prevWatermark time.Time
	if err := testPool.QueryRow(ctx, `SELECT watermark_at FROM task_usage_rollup_state WHERE id = 1`).Scan(&prevWatermark); err != nil {
		t.Fatalf("read prev watermark: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE task_usage_rollup_state SET watermark_at = $1 WHERE id = 1`, prevWatermark)
	})

	if _, err := testPool.Exec(ctx, `
		UPDATE task_usage_rollup_state
		   SET watermark_at = '2020-01-01 00:00:00+00', last_error = NULL
		 WHERE id = 1
	`); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}

	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily()`); err != nil {
		t.Fatalf("rollup_task_usage_daily: %v", err)
	}

	var newWatermark time.Time
	var lastError *string
	if err := testPool.QueryRow(ctx, `SELECT watermark_at, last_error FROM task_usage_rollup_state WHERE id = 1`).Scan(&newWatermark, &lastError); err != nil {
		t.Fatalf("read new watermark: %v", err)
	}
	if lastError != nil {
		t.Fatalf("rollup recorded error: %s", *lastError)
	}

	// New watermark must be near now() - 5 min. Allow a wide window
	// (±2 min) so this isn't flaky on slow CI.
	expected := time.Now().UTC().Add(-5 * time.Minute)
	delta := newWatermark.Sub(expected)
	if delta < -2*time.Minute || delta > 2*time.Minute {
		t.Errorf("watermark %s not within 2min of expected %s (delta %s)", newWatermark, expected, delta)
	}
}

// TestRollupTaskUsageDaily_InvalidationOnReassign verifies that the
// trigger-driven dirty-bucket queue handles task reassignment between
// runtimes (the ReassignTasksToRuntime path used during runtime merge).
// Without invalidation the rollup would keep attributing usage to the
// old runtime; the raw fallback would not — so the two read paths would
// silently disagree.
func TestRollupTaskUsageDaily_InvalidationOnReassign(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	oldRuntimeID := handlerTestRuntimeID(t)
	// Spin up a second runtime to receive the reassigned task.
	var newRuntimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
)
VALUES ($1, NULL, 'reassign-target', 'cloud', 'reassign-target', 'online', '{}'::jsonb, '{}'::jsonb, now())
RETURNING id
`, testWorkspaceID).Scan(&newRuntimeID); err != nil {
		t.Fatalf("create dest runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_id, creator_type)
VALUES ($1, 'reassign test', $2, 'member')
RETURNING id
`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	day := time.Date(2021, 3, 14, 0, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
VALUES ($1, $2, $3, 'completed', $4)
RETURNING id
`, agentID, issueID, oldRuntimeID, day.Add(time.Hour)).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at, updated_at)
VALUES ($1, 'claude', 'm-reassign', 700, 70, $2, $2)
`, taskID, day.Add(time.Hour)); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_daily WHERE bucket_date = $1::date AND model = 'm-reassign'`, day)
		testPool.Exec(ctx, `DELETE FROM task_usage_daily_dirty WHERE bucket_date = $1::date AND model = 'm-reassign'`, day)
	})

	// Initial roll-up: usage should attach to OLD runtime.
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)`); err != nil {
		t.Fatalf("initial rollup: %v", err)
	}
	var oldTokens, newTokens int64
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-reassign'`, oldRuntimeID, day).Scan(&oldTokens)
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-reassign'`, newRuntimeID, day).Scan(&newTokens)
	if oldTokens != 700 || newTokens != 0 {
		t.Fatalf("initial: expected old=700 new=0, got old=%d new=%d", oldTokens, newTokens)
	}

	// Trigger should enqueue both old + new buckets.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET runtime_id = $1 WHERE id = $2`, newRuntimeID, taskID); err != nil {
		t.Fatalf("reassign task: %v", err)
	}
	var dirtyCount int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_daily_dirty WHERE bucket_date = $1::date AND model = 'm-reassign'`, day).Scan(&dirtyCount)
	if dirtyCount != 2 {
		t.Fatalf("expected 2 dirty entries (old+new runtime), got %d", dirtyCount)
	}

	// Re-run rollup. Old bucket should be deleted (no source rows left),
	// new bucket should receive the moved usage.
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)`); err != nil {
		t.Fatalf("rollup after reassign: %v", err)
	}
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-reassign'`, oldRuntimeID, day).Scan(&oldTokens)
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-reassign'`, newRuntimeID, day).Scan(&newTokens)
	if oldTokens != 0 || newTokens != 700 {
		t.Fatalf("after reassign: expected old=0 new=700, got old=%d new=%d", oldTokens, newTokens)
	}
	// Dirty queue should be drained.
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_daily_dirty WHERE bucket_date = $1::date AND model = 'm-reassign'`, day).Scan(&dirtyCount)
	if dirtyCount != 0 {
		t.Errorf("expected dirty queue drained, got %d entries", dirtyCount)
	}
}

// TestRollupTaskUsageDaily_InvalidationOnIssueDelete verifies that
// cascade delete (issue → agent_task_queue → task_usage) clears the
// matching daily rows via the trigger-driven dirty queue.
func TestRollupTaskUsageDaily_InvalidationOnIssueDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := handlerTestRuntimeID(t)
	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_id, creator_type)
VALUES ($1, 'delete test', $2, 'member') RETURNING id
`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	day := time.Date(2021, 7, 4, 0, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
VALUES ($1, $2, $3, 'completed', $4) RETURNING id
`, agentID, issueID, runtimeID, day.Add(time.Hour)).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at, updated_at)
VALUES ($1, 'claude', 'm-delete', 500, 50, $2, $2)
`, taskID, day.Add(time.Hour)); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_daily WHERE bucket_date = $1::date AND model = 'm-delete'`, day)
		testPool.Exec(ctx, `DELETE FROM task_usage_daily_dirty WHERE bucket_date = $1::date AND model = 'm-delete'`, day)
	})

	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)`); err != nil {
		t.Fatalf("initial rollup: %v", err)
	}
	var tokens int64
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-delete'`, runtimeID, day).Scan(&tokens)
	if tokens != 500 {
		t.Fatalf("initial: expected 500, got %d", tokens)
	}

	// Cascade delete via issue. Trigger fires on agent_task_queue BEFORE
	// DELETE — that's when the task_usage children + issue parent are
	// still readable inside the same statement.
	if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	var dirtyCount int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_daily_dirty WHERE bucket_date = $1::date AND model = 'm-delete'`, day).Scan(&dirtyCount)
	if dirtyCount == 0 {
		t.Fatalf("expected dirty entry after cascade delete, got 0")
	}

	// Re-run rollup: bucket should be deleted because no source rows exist.
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)`); err != nil {
		t.Fatalf("rollup after delete: %v", err)
	}
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-delete'`, runtimeID, day).Scan(&tokens)
	if tokens != 0 {
		t.Errorf("after issue delete: expected 0 (bucket cleared), got %d", tokens)
	}
}

// TestRollupTaskUsageDaily_WorkspaceMismatch constructs an atq row whose
// agent.workspace_id != issue.workspace_id and verifies that the rollup
// resolves workspace_id consistently from `agent` across triggers,
// dirty_from_updates, and recompute. If any of those paths leaked back
// to the issue.workspace_id the dirty queue would be misaligned with
// the recompute join and the bucket would either be silently dropped
// (recompute returns 0 rows → deleted_empty branch fires) or attributed
// to the wrong workspace.
//
// The schema does not enforce agent.workspace_id == issue.workspace_id,
// so this canary keeps the alignment honest as the schema evolves.
func TestRollupTaskUsageDaily_WorkspaceMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create a foreign workspace + a runtime + an agent there.
	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug) VALUES ('ws-mismatch', 'ws-mismatch-' || gen_random_uuid()::text) RETURNING id
`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})
	var foreignRuntimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
)
VALUES ($1, NULL, 'mismatch-rt', 'cloud', 'mismatch-rt', 'online', '{}'::jsonb, '{}'::jsonb, now())
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
VALUES ($1, 'mismatch-agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
RETURNING id
`, foreignWorkspaceID, foreignRuntimeID, testUserID).Scan(&foreignAgentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}

	// Issue lives in the *primary* test workspace, agent in foreign one.
	var issueID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_id, creator_type)
VALUES ($1, 'mismatch test', $2, 'member') RETURNING id
`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	day := time.Date(2021, 9, 9, 0, 0, 0, 0, time.UTC)
	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
VALUES ($1, $2, $3, 'completed', $4) RETURNING id
`, foreignAgentID, issueID, foreignRuntimeID, day.Add(time.Hour)).Scan(&taskID); err != nil {
		t.Fatalf("insert atq: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at, updated_at)
VALUES ($1, 'claude', 'm-mismatch', 333, 33, $2, $2)
`, taskID, day.Add(time.Hour)); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_usage_daily WHERE bucket_date = $1::date AND model = 'm-mismatch'`, day)
		testPool.Exec(ctx, `DELETE FROM task_usage_daily_dirty WHERE bucket_date = $1::date AND model = 'm-mismatch'`, day)
	})

	// Rollup. The bucket must be attributed to FOREIGN workspace
	// (agent.workspace_id), not the primary one (issue.workspace_id).
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)`); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	var foreignTokens, primaryTokens int64
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE workspace_id = $1 AND bucket_date = $2::date AND model = 'm-mismatch'`, foreignWorkspaceID, day).Scan(&foreignTokens)
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE workspace_id = $1 AND bucket_date = $2::date AND model = 'm-mismatch'`, testWorkspaceID, day).Scan(&primaryTokens)
	if foreignTokens != 333 {
		t.Fatalf("expected foreign workspace bucket = 333, got %d", foreignTokens)
	}
	if primaryTokens != 0 {
		t.Errorf("expected primary workspace bucket = 0, got %d", primaryTokens)
	}

	// Now reassign atq.runtime_id within the foreign workspace and
	// verify the trigger / recompute pair still agree on workspace_id.
	var foreignRuntime2ID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
)
VALUES ($1, NULL, 'mismatch-rt2', 'cloud', 'mismatch-rt2', 'online', '{}'::jsonb, '{}'::jsonb, now())
RETURNING id
`, foreignWorkspaceID).Scan(&foreignRuntime2ID); err != nil {
		t.Fatalf("create foreign runtime 2: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET runtime_id = $1 WHERE id = $2`, foreignRuntime2ID, taskID); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if _, err := testPool.Exec(ctx, `SELECT rollup_task_usage_daily_window('-infinity'::timestamptz, 'infinity'::timestamptz)`); err != nil {
		t.Fatalf("rollup after reassign: %v", err)
	}
	var oldRTTokens, newRTTokens int64
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-mismatch'`, foreignRuntimeID, day).Scan(&oldRTTokens)
	testPool.QueryRow(ctx, `SELECT COALESCE(SUM(input_tokens),0) FROM task_usage_daily WHERE runtime_id = $1 AND bucket_date = $2::date AND model = 'm-mismatch'`, foreignRuntime2ID, day).Scan(&newRTTokens)
	if oldRTTokens != 0 || newRTTokens != 333 {
		t.Fatalf("after reassign in mismatched ws: expected old=0 new=333, got old=%d new=%d", oldRTTokens, newRTTokens)
	}
}
