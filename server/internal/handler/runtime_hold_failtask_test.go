package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestFailTaskSessionLimitHoldsRuntimeAndRetries pins the FailTask detection
// glue end-to-end: when a task on a runtime fails with a Claude session-limit
// message, FailTask must (1) place the runtime on hold with a parsed
// hold_until, and (2) auto-queue a retry that stays gated behind the hold.
//
// This is the integration point the parser / sweeper / resume-API unit tests
// never reach: deleting the `failureReason == "session_limit"` block from
// FailTask leaves every one of those tests green, so without this test the
// feature's actual wiring could regress unnoticed.
func TestFailTaskSessionLimitHoldsRuntimeAndRetries(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "Session Limit FailTask Agent", []byte("{}"))

	// Start from a clean (unheld) runtime and restore it afterwards so the
	// shared handler-test runtime is not left on hold for later tests.
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET hold_until = NULL, hold_reason = NULL WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("reset runtime hold: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET hold_until = NULL, hold_reason = NULL WHERE id = $1`, runtimeID)
	})

	// Seed an issue assigned to the agent and a running task on the runtime.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		VALUES ($1, 'session limit failtask test', 'in_progress', 'none', 'member', $2, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now(), now())
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create running task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	svc := service.NewTaskService(queries, testPool, hub, bus)

	// Fail the task with a Claude session-limit message. Pass an empty
	// failureReason so FailTask runs the classifier itself — this also
	// exercises the Classify("...session limit...resets...") == session_limit
	// path that the detection block keys off.
	const msg = "You've hit your session limit · resets 11:59pm (UTC)"
	if _, err := svc.FailTask(ctx, parseUUID(taskID), msg, "", "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	// (1) The runtime must be on hold with a parsed expiry and the
	// session_limit reason.
	var holdUntilSet bool
	var holdReason *string
	if err := testPool.QueryRow(ctx, `
		SELECT hold_until IS NOT NULL, hold_reason FROM agent_runtime WHERE id = $1
	`, runtimeID).Scan(&holdUntilSet, &holdReason); err != nil {
		t.Fatalf("read runtime hold: %v", err)
	}
	if !holdUntilSet {
		t.Fatal("FailTask did not set hold_until on the runtime after a session-limit failure")
	}
	if holdReason == nil || *holdReason != "session_limit" {
		t.Fatalf("expected hold_reason 'session_limit', got %v", holdReason)
	}

	// (2) A retry must have been queued for the failed task, gated behind
	// the hold (the queued clone carries the same runtime_id, which both
	// claim paths skip while the hold is active).
	var retryCount int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE parent_task_id = $1 AND status = 'queued'
	`, taskID).Scan(&retryCount); err != nil {
		t.Fatalf("count retry tasks: %v", err)
	}
	if retryCount != 1 {
		t.Fatalf("expected exactly 1 queued retry task after session-limit failure, got %d", retryCount)
	}
}

// TestFailTaskSessionLimitUnparseableResetDoesNotRetry pins the retry/hold
// coupling. A message can classify as session_limit (it contains "session
// limit" and "reset") yet carry no parseable reset time, so no bounded hold
// can be placed. In that case FailTask must NOT auto-retry: a retry clone
// would be claimed immediately (the runtime is not held) and hot-loop the
// task until max_attempts. Asserts the runtime stays un-held AND no retry is
// queued — the inverse of the happy path above.
func TestFailTaskSessionLimitUnparseableResetDoesNotRetry(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "Session Limit Unparseable Agent", []byte("{}"))

	// Start from a clean (unheld) runtime and restore it afterwards so the
	// shared handler-test runtime is not left on hold for later tests.
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET hold_until = NULL, hold_reason = NULL WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("reset runtime hold: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET hold_until = NULL, hold_reason = NULL WHERE id = $1`, runtimeID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		VALUES ($1, 'session limit unparseable test', 'in_progress', 'none', 'member', $2, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now(), now())
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create running task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	svc := service.NewTaskService(queries, testPool, hub, bus)

	// "session limit" + "reset" classifies as session_limit, but there is no
	// "resets H:MM am/pm (UTC)" token for ParseSessionLimitResetTime to read.
	const msg = "You've hit your session limit · reset time unavailable"
	if _, err := svc.FailTask(ctx, parseUUID(taskID), msg, "", "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	// (1) The runtime must stay un-held: an unbounded hold would strand it.
	var holdUntilSet bool
	if err := testPool.QueryRow(ctx, `
		SELECT hold_until IS NOT NULL FROM agent_runtime WHERE id = $1
	`, runtimeID).Scan(&holdUntilSet); err != nil {
		t.Fatalf("read runtime hold: %v", err)
	}
	if holdUntilSet {
		t.Fatal("runtime should not be held when the session-limit reset time is unparseable")
	}

	// (2) No retry must have been queued — the coupling that prevents the
	// hot-loop. This is the assertion that fails if retryableReasons is
	// consulted without the runtimeOnHold gate.
	var retryCount int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE parent_task_id = $1 AND status = 'queued'
	`, taskID).Scan(&retryCount); err != nil {
		t.Fatalf("count retry tasks: %v", err)
	}
	if retryCount != 0 {
		t.Fatalf("expected no queued retry task when runtime is not held, got %d", retryCount)
	}
}
