package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestAutoPauseCircuitBreaker is the FIR-2476 regression, updated for FIR-2717:
// a runtime that keeps hitting a usage cap (no parseable reset time) must back
// off with a growing pause WITHOUT posting routine-pause comments, then — after
// autoPauseCircuitLimit consecutive auto-pauses without an intervening success
// — stop auto-resuming (unpause_at NULL) and post the manual-intervention
// notice exactly once. A later success resets the counter.
func TestAutoPauseCircuitBreaker(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping circuit-breaker integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	wsID := runtimeAccountTestWSID
	runtimeID := runtimeAccountTestRuntimeID

	var agentID, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'auto-pause-circuit-agent', 'local', $2)
		RETURNING id`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'auto-pause-circuit-issue', 'member', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = pool.Exec(bg, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		_, _ = pool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = pool.Exec(bg, `UPDATE agent_runtime SET paused_at = NULL, unpause_at = NULL, pause_reason = NULL, auto_pause_count = 0 WHERE id = $1`, runtimeID)
	})
	// Start from a clean counter — the shared runtime fixture may carry state
	// from a sibling test in this package.
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET auto_pause_count = 0, paused_at = NULL, unpause_at = NULL WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("reset runtime fixture: %v", err)
	}

	// A failed task carrying a usage-cap error with no parseable reset time.
	var taskID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, priority,
			dispatched_at, started_at, completed_at, error, failure_reason
		)
		VALUES ($1, $2, $3, 'failed', 0, now(), now(), now(), $4, 'agent_error')
		RETURNING id`,
		agentID, issueID, runtimeID, "You've hit your org's monthly usage limit").Scan(&taskID); err != nil {
		t.Fatalf("insert failed task: %v", err)
	}

	upstream := db.New(pool)
	loadTask := func() db.AgentTaskQueue {
		row, err := upstream.GetAgentTask(ctx, taskID)
		if err != nil {
			t.Fatalf("load task: %v", err)
		}
		return row
	}
	readRuntime := func() (int32, pgtype.Timestamptz, pgtype.Timestamptz) {
		var count int32
		var pausedAt, unpauseAt pgtype.Timestamptz
		if err := pool.QueryRow(ctx,
			`SELECT auto_pause_count, paused_at, unpause_at FROM agent_runtime WHERE id = $1`, runtimeID,
		).Scan(&count, &pausedAt, &unpauseAt); err != nil {
			t.Fatalf("read runtime: %v", err)
		}
		return count, pausedAt, unpauseAt
	}
	countComments := func() int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_id = $2`, issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count comments: %v", err)
		}
		return n
	}

	svc := &Service{Cerebro: queries} // nil TaskSvc/Bus: the failed task is never suspended.

	// Cycles below the limit: counter climbs, unpause_at stays scheduled, the
	// fallback backoff grows, and each pause posts the issue-facing reason.
	for cycle := int32(1); cycle < autoPauseCircuitLimit; cycle++ {
		before := time.Now()
		if !svc.MaybeAutoPauseOnFailure(ctx, loadTask()) {
			t.Fatalf("cycle %d: expected a pause", cycle)
		}
		count, pausedAt, unpauseAt := readRuntime()
		if count != cycle {
			t.Fatalf("cycle %d: auto_pause_count=%d, want %d", cycle, count, cycle)
		}
		if !pausedAt.Valid {
			t.Fatalf("cycle %d: runtime should be paused", cycle)
		}
		if !unpauseAt.Valid {
			t.Fatalf("cycle %d: unpause_at should be scheduled (circuit not yet open)", cycle)
		}
		// unpause_at ≈ now + growingBackoff(cycle); allow generous slack for
		// the round-trip rather than asserting to the second.
		want := growingBackoff(cycle)
		got := unpauseAt.Time.Sub(before)
		if got < want-time.Minute || got > want+time.Minute {
			t.Fatalf("cycle %d: backoff %s, want ≈%s", cycle, got, want)
		}
		if c := countComments(); c != int(cycle) {
			t.Fatalf("cycle %d: expected %d notice(s), got %d", cycle, cycle, c)
		}
	}

	// The trip cycle: counter hits the limit, unpause_at clears (manual-only),
	// and the trip reason is posted.
	if !svc.MaybeAutoPauseOnFailure(ctx, loadTask()) {
		t.Fatal("trip cycle: expected a pause")
	}
	count, pausedAt, unpauseAt := readRuntime()
	if count != autoPauseCircuitLimit {
		t.Fatalf("trip cycle: count=%d, want %d", count, autoPauseCircuitLimit)
	}
	if !pausedAt.Valid {
		t.Fatal("trip cycle: runtime should be paused")
	}
	if unpauseAt.Valid {
		t.Fatal("trip cycle: unpause_at must be NULL once the circuit is open")
	}
	if c := countComments(); c != int(autoPauseCircuitLimit) {
		t.Fatalf("trip cycle: expected %d notices, got %d", autoPauseCircuitLimit, c)
	}

	// Past the trip: circuit stays open and still records why this attempt
	// failed without scheduling auto-resume.
	if !svc.MaybeAutoPauseOnFailure(ctx, loadTask()) {
		t.Fatal("post-trip cycle: expected a pause")
	}
	count, _, unpauseAt = readRuntime()
	if count != autoPauseCircuitLimit+1 {
		t.Fatalf("post-trip: count=%d, want %d", count, autoPauseCircuitLimit+1)
	}
	if unpauseAt.Valid {
		t.Fatal("post-trip: unpause_at must stay NULL")
	}
	if c := countComments(); c != int(autoPauseCircuitLimit+1) {
		t.Fatalf("post-trip: expected %d notices, got %d", autoPauseCircuitLimit+1, c)
	}

	// A successful run resets the counter.
	svc.ResetAutoPauseCount(ctx, runtimeID)
	if count, _, _ := readRuntime(); count != 0 {
		t.Fatalf("after reset: count=%d, want 0", count)
	}
}
