package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// JEH-1476 regression: a failure on one runtime must pause ONLY that runtime.
// Sibling runtimes on the same workspace must keep their paused_at NULL and
// their in-flight tasks untouched, so a rate-limit on one local MacBook
// runtime cannot stop work on the other healthy runtimes.
//
// Tests are DB-backed (same shared pool as resume_scope_test.go) and skip
// silently when DATABASE_URL is unreachable.

// TestSuspendActiveTasksForRuntime_OnlyFailsTargetRuntime pins the SQL gate
// that powers PauseRuntime's task-suspension step. The bug we're guarding
// against is a pause "leaking" into other runtimes' in-flight tasks.
func TestSuspendActiveTasksForRuntime_OnlyFailsTargetRuntime(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping pause-isolation integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	wsID := runtimeAccountTestWSID

	// Create runtime B (A is the shared fixture runtimeAccountTestRuntimeID).
	var runtimeB pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1, 'pause-isolation-host-b', 'isolation-runtime-b', 'local', 'claude', 'online', '', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&runtimeB); err != nil {
		t.Fatalf("create runtime B: %v", err)
	}
	runtimeA := runtimeAccountTestRuntimeID

	var agentA, agentB, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'pause-isolation-agent-a', 'local', $2)
		RETURNING id`, wsID, runtimeA).Scan(&agentA); err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'pause-isolation-agent-b', 'local', $2)
		RETURNING id`, wsID, runtimeB).Scan(&agentB); err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'pause-isolation-issue', 'member', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentA, agentB)
		_, _ = pool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id IN ($1, $2)`, agentA, agentB)
		_, _ = pool.Exec(bg, `UPDATE agent_runtime SET paused_at = NULL, unpause_at = NULL, pause_reason = NULL WHERE id = $1`, runtimeA)
		_, _ = pool.Exec(bg, `DELETE FROM agent_runtime WHERE id = $1`, runtimeB)
	})

	insertRunningTask := func(agentID, runtimeID pgtype.UUID) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, runtime_id, status, priority,
				dispatched_at, started_at
			)
			VALUES ($1, $2, $3, 'running', 0, now(), now())
			RETURNING id`,
			agentID, issueID, runtimeID).Scan(&id)
		if err != nil {
			t.Fatalf("insert running task: %v", err)
		}
		return id
	}

	taskA := insertRunningTask(agentA, runtimeA)
	taskB := insertRunningTask(agentB, runtimeB)

	suspended, err := queries.SuspendActiveTasksForRuntime(ctx, runtimeA)
	if err != nil {
		t.Fatalf("SuspendActiveTasksForRuntime: %v", err)
	}

	// Exactly one task suspended, and it's runtime A's task.
	if len(suspended) != 1 {
		t.Fatalf("expected 1 suspended task, got %d", len(suspended))
	}
	if suspended[0].ID != taskA {
		t.Fatalf("suspended wrong task: got %v, want %v (taskA)", suspended[0].ID, taskA)
	}

	// Verify B's task is still running and untouched.
	var (
		bStatus        string
		bFailureReason pgtype.Text
		bCompletedAt   pgtype.Timestamptz
	)
	if err := pool.QueryRow(ctx,
		`SELECT status, failure_reason, completed_at FROM agent_task_queue WHERE id = $1`, taskB,
	).Scan(&bStatus, &bFailureReason, &bCompletedAt); err != nil {
		t.Fatalf("read task B: %v", err)
	}
	if bStatus != "running" {
		t.Errorf("task B status = %q, want 'running' — pause leaked across runtimes", bStatus)
	}
	if bFailureReason.Valid {
		t.Errorf("task B failure_reason = %q, want NULL", bFailureReason.String)
	}
	if bCompletedAt.Valid {
		t.Errorf("task B completed_at set unexpectedly: %v", bCompletedAt.Time)
	}
}

// TestPauseRuntime_DoesNotPauseSiblingRuntime is the end-to-end pause
// invariant: pausing A leaves B's paused_at NULL. Exercises the Service so
// any future "convenience" change that fans pause out to siblings (by
// daemon_id, owner_id, workspace_id, account, ...) would trip this.
//
// TaskSvc is intentionally nil — when the target runtime has no in-flight
// tasks (the common auto-pause-after-task-fail case), the Service skips the
// HandleFailedTasks call entirely, so a nil receiver is safe.
func TestPauseRuntime_DoesNotPauseSiblingRuntime(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping pause-isolation integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	wsID := runtimeAccountTestWSID

	var runtimeB pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1, 'pause-isolation-host-sibling', 'isolation-sibling-b', 'local', 'claude', 'online', '', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&runtimeB); err != nil {
		t.Fatalf("create sibling runtime: %v", err)
	}
	runtimeA := runtimeAccountTestRuntimeID
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `UPDATE agent_runtime SET paused_at = NULL, unpause_at = NULL, pause_reason = NULL WHERE id = $1`, runtimeA)
		_, _ = pool.Exec(bg, `DELETE FROM agent_runtime WHERE id = $1`, runtimeB)
	})

	svc := &Service{Cerebro: queries} // TaskSvc + Bus nil — no in-flight work, no broadcasts.

	if _, err := svc.PauseRuntime(ctx, runtimeA, handler.RuntimePauseOptions{
		Reason:    "auto",
		UnpauseAt: time.Now().Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("PauseRuntime(A): %v", err)
	}

	readPauseFields := func(id pgtype.UUID) (paused, unpause pgtype.Timestamptz, reason pgtype.Text) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`SELECT paused_at, unpause_at, pause_reason FROM agent_runtime WHERE id = $1`, id,
		).Scan(&paused, &unpause, &reason); err != nil {
			t.Fatalf("read pause fields for %v: %v", id, err)
		}
		return
	}

	aPaused, aUnpause, aReason := readPauseFields(runtimeA)
	if !aPaused.Valid {
		t.Errorf("runtime A paused_at = NULL, want set")
	}
	if !aUnpause.Valid {
		t.Errorf("runtime A unpause_at = NULL, want set")
	}
	if aReason.String != "auto" {
		t.Errorf("runtime A pause_reason = %q, want 'auto'", aReason.String)
	}

	bPaused, bUnpause, bReason := readPauseFields(runtimeB)
	if bPaused.Valid {
		t.Errorf("runtime B paused_at = %v, want NULL — pause leaked across runtimes", bPaused.Time)
	}
	if bUnpause.Valid {
		t.Errorf("runtime B unpause_at = %v, want NULL", bUnpause.Time)
	}
	if bReason.Valid {
		t.Errorf("runtime B pause_reason = %q, want NULL", bReason.String)
	}
}

// TestMaybeAutoPauseOnFailure_OnlyTouchesFailingRuntime is the public-API
// invariant: a rate-limit / quota / 401 failure on runtime A must pause A
// and ONLY A. The triggering task is reclassified, B's task is untouched,
// and B's pause fields stay NULL.
//
// This is the regression Jesper hit on 2026-05-12: an auto-pause that fanned
// out to every runtime instead of staying on the one that hit the limit.
func TestMaybeAutoPauseOnFailure_OnlyTouchesFailingRuntime(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping pause-isolation integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	wsID := runtimeAccountTestWSID

	var runtimeB pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1, 'auto-pause-isolation-host', 'auto-pause-sibling-b', 'local', 'claude', 'online', '', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&runtimeB); err != nil {
		t.Fatalf("create sibling runtime: %v", err)
	}
	runtimeA := runtimeAccountTestRuntimeID

	var agentA, agentB, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'auto-pause-isolation-agent-a', 'local', $2)
		RETURNING id`, wsID, runtimeA).Scan(&agentA); err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'auto-pause-isolation-agent-b', 'local', $2)
		RETURNING id`, wsID, runtimeB).Scan(&agentB); err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'auto-pause-isolation-issue', 'member', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentA, agentB)
		_, _ = pool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id IN ($1, $2)`, agentA, agentB)
		_, _ = pool.Exec(bg, `UPDATE agent_runtime SET paused_at = NULL, unpause_at = NULL, pause_reason = NULL WHERE id = $1`, runtimeA)
		_, _ = pool.Exec(bg, `DELETE FROM agent_runtime WHERE id = $1`, runtimeB)
	})

	// Mirror the production flow: FailTask flips the task to 'failed' with
	// failure_reason='agent_error' first, then calls MaybeAutoPauseOnFailure.
	// We insert the failed row directly so we don't need the whole TaskService
	// wired up just to land at the same DB state.
	insertFailedTask := func(agentID, runtimeID pgtype.UUID, errText string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, runtime_id, status, priority,
				dispatched_at, started_at, completed_at, error, failure_reason
			)
			VALUES ($1, $2, $3, 'failed', 0, now(), now(), now(), $4, 'agent_error')
			RETURNING id`,
			agentID, issueID, runtimeID, errText).Scan(&id)
		if err != nil {
			t.Fatalf("insert failed task: %v", err)
		}
		return id
	}

	// A's failing task hits Anthropic's monthly cap — should trigger auto-pause.
	// B's failing task is a generic agent bug — must NOT trigger auto-pause and
	// must NOT have its runtime touched by A's pause.
	taskA := insertFailedTask(agentA, runtimeA, "You've hit your org's monthly usage limit")
	taskB := insertFailedTask(agentB, runtimeB, "tool execution failed: command not found")

	// Read back the task rows in their post-FailTask shape so we hand the
	// service the same struct production hands it (the db pkg's
	// AgentTaskQueue, populated by sqlc from the failed row we just wrote).
	loadUpstreamTask := func(id pgtype.UUID) db.AgentTaskQueue {
		t.Helper()
		upstream := db.New(pool)
		row, err := upstream.GetAgentTask(ctx, id)
		if err != nil {
			t.Fatalf("load upstream task %v: %v", id, err)
		}
		return row
	}

	svc := &Service{Cerebro: queries} // No TaskSvc/Bus needed: A's only task is already failed.

	if !svc.MaybeAutoPauseOnFailure(ctx, loadUpstreamTask(taskA)) {
		t.Fatal("MaybeAutoPauseOnFailure returned false for a monthly-cap error; expected true")
	}
	// B's task is a generic agent error; the service must NOT pause its runtime.
	if svc.MaybeAutoPauseOnFailure(ctx, loadUpstreamTask(taskB)) {
		t.Error("MaybeAutoPauseOnFailure returned true for a non-rate-limit error on runtime B")
	}

	readPaused := func(id pgtype.UUID) pgtype.Timestamptz {
		t.Helper()
		var p pgtype.Timestamptz
		if err := pool.QueryRow(ctx,
			`SELECT paused_at FROM agent_runtime WHERE id = $1`, id,
		).Scan(&p); err != nil {
			t.Fatalf("read paused_at for %v: %v", id, err)
		}
		return p
	}
	if !readPaused(runtimeA).Valid {
		t.Error("runtime A paused_at = NULL after monthly-cap failure; expected pause")
	}
	if readPaused(runtimeB).Valid {
		t.Error("runtime B paused_at set; auto-pause leaked across runtimes")
	}

	// Triggering task on A is reclassified to 'rate_limit' so the unpause
	// sweeper can resume it; B's task keeps its original 'agent_error'.
	readFailureReason := func(id pgtype.UUID) string {
		t.Helper()
		var r pgtype.Text
		if err := pool.QueryRow(ctx,
			`SELECT failure_reason FROM agent_task_queue WHERE id = $1`, id,
		).Scan(&r); err != nil {
			t.Fatalf("read failure_reason for %v: %v", id, err)
		}
		return r.String
	}
	if got := readFailureReason(taskA); got != "rate_limit" {
		t.Errorf("task A failure_reason = %q, want 'rate_limit' (reclassified by auto-pause)", got)
	}
	if got := readFailureReason(taskB); got != "agent_error" {
		t.Errorf("task B failure_reason = %q, want 'agent_error' (untouched)", got)
	}
}
