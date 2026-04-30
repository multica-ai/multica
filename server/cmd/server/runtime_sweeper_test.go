package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newSweepTaskSvc builds a minimal TaskService for sweeper tests. The
// realtime hub is unused on the failure path so we pass nil.
func newSweepTaskSvc(queries *db.Queries, bus *events.Bus) *service.TaskService {
	return service.NewTaskService(queries, nil, bus)
}

// setupSweeperTestFixture creates an issue and a task in the given status with
// timestamps old enough to trigger the sweeper. Returns (issueID, agentID, taskID).
func setupSweeperTestFixture(t *testing.T, taskStatus string) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	// Find the integration test agent
	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create an issue assigned to the agent
	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Sweeper test issue', 'todo', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	// Create a task in the desired status with old timestamps
	var taskID string
	switch taskStatus {
	case "running":
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
			VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
			RETURNING id
		`, agentID, runtimeID, issueID).Scan(&taskID)
	case "dispatched":
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at)
			VALUES ($1, $2, $3, 'dispatched', 0, now() - interval '10 minutes')
			RETURNING id
		`, agentID, runtimeID, issueID).Scan(&taskID)
	}
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}

	// Set agent status to "working"
	_, err = testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID)
	if err != nil {
		t.Fatalf("failed to set agent status: %v", err)
	}

	return issueID, agentID, taskID
}

func cleanupSweeperFixture(t *testing.T, issueID, agentID string) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID)
}

// TestSweepStaleTasksBroadcastsWithWorkspaceID verifies that when the task sweeper
// fails a stale running task, the task:failed event is broadcast with the correct
// WorkspaceID so it reaches frontend WebSocket clients (events without WorkspaceID
// are silently dropped by the WS listener — that was the original bug).
func TestSweepStaleTasksBroadcastsWithWorkspaceID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	bus := events.New()

	// Capture task:failed events to verify WorkspaceID is set
	var taskEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("task:failed", func(e events.Event) {
		mu.Lock()
		taskEvents = append(taskEvents, e)
		mu.Unlock()
	})

	// Use very short timeouts to trigger the sweep on our test task
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0, // 1 second — our task is 3 hours old
	})
	if err != nil {
		t.Fatalf("FailStaleTasks query failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale task to be failed")
	}

	// Verify our task was included
	found := false
	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task %s to be in failed tasks list", taskID)
	}

	// Call handleSweptFailures — this is what we're testing
	handleSweptFailures(context.Background(), queries, newSweepTaskSvc(queries, bus), failedTasks, "task timed out")

	// Verify the event was published with WorkspaceID (the core of the bug fix)
	mu.Lock()
	defer mu.Unlock()
	var foundEvent bool
	for _, e := range taskEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["task_id"] == taskID {
			if e.WorkspaceID == "" {
				t.Fatal("task:failed event is missing WorkspaceID — this was the original bug")
			}
			if e.WorkspaceID != testWorkspaceID {
				t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, e.WorkspaceID)
			}
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("expected task:failed event for task %s", taskID)
	}

	// Verify DB: task should be failed
	var status string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got '%s'", status)
	}
}

// TestSweepStaleTasksReconcileAgentStatus verifies that after the sweeper fails
// stale tasks, the agent status is reconciled from "working" back to "idle".
func TestSweepStaleTasksReconcileAgentStatus(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, _ := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	bus := events.New()

	// Capture agent:status events
	var agentStatusEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("agent:status", func(e events.Event) {
		mu.Lock()
		agentStatusEvents = append(agentStatusEvents, e)
		mu.Unlock()
	})

	// Fail stale tasks with short timeout
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale task")
	}

	handleSweptFailures(context.Background(), queries, newSweepTaskSvc(queries, bus), failedTasks, "task timed out")

	// Verify agent status is now "idle" in DB
	var agentStatus string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus)
	if err != nil {
		t.Fatalf("failed to query agent status: %v", err)
	}
	if agentStatus != "idle" {
		t.Fatalf("expected agent status 'idle', got '%s'", agentStatus)
	}

	// Verify agent:status event was published with correct WorkspaceID
	mu.Lock()
	defer mu.Unlock()
	if len(agentStatusEvents) == 0 {
		t.Fatal("expected agent:status event to be published")
	}
	lastEvent := agentStatusEvents[len(agentStatusEvents)-1]
	if lastEvent.WorkspaceID == "" {
		t.Fatal("agent:status event should have WorkspaceID set")
	}
	if lastEvent.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, lastEvent.WorkspaceID)
	}
}

// TestSweepDispatchedStaleTask verifies the sweeper handles dispatched tasks
// stuck beyond the dispatch timeout.
func TestSweepDispatchedStaleTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	bus := events.New()

	// Capture task:failed events
	var taskEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("task:failed", func(e events.Event) {
		mu.Lock()
		taskEvents = append(taskEvents, e)
		mu.Unlock()
	})

	// Fail stale tasks — dispatch timeout of 1 second (our task is 10 minutes old)
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs: 1.0,
		RunningTimeoutSecs:  9000.0,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale dispatched task")
	}

	handleSweptFailures(context.Background(), queries, newSweepTaskSvc(queries, bus), failedTasks, "task timed out")

	// Verify DB: task should be failed
	var status string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got '%s'", status)
	}

	// Verify task:failed event was published WITH WorkspaceID
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, e := range taskEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["task_id"] == taskID {
			if e.WorkspaceID == "" {
				t.Fatal("task:failed event is missing WorkspaceID — this was the bug")
			}
			if e.WorkspaceID != testWorkspaceID {
				t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, e.WorkspaceID)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task:failed event for task %s", taskID)
	}

	// Verify agent status reconciled to idle
	var agentStatus string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus)
	if err != nil {
		t.Fatalf("failed to query agent: %v", err)
	}
	if agentStatus != "idle" {
		t.Fatalf("expected agent status 'idle' after sweep, got '%s'", agentStatus)
	}
}

// TestSweepResetsInProgressIssueToTodo verifies the core fix: when the sweeper
// force-fails a stale task whose issue is still in_progress (because the daemon
// crashed mid-run), the issue is reset back to todo so the daemon can re-queue it.
//
// Without this fix the issue stays in_progress permanently — the agent never runs
// to update the status because it was never dispatched.
func TestSweepResetsInProgressIssueToTodo(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	// Use the same agent/runtime as the other sweeper tests.
	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create an issue already in in_progress (simulates a daemon crash mid-run).
	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Stuck in_progress issue', 'in_progress', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Create a stale running task for the issue (3 hours old — beyond any timeout).
	// attempt=max_attempts so the retry guard rejects it; this exercises the
	// reset-stuck-issue branch in HandleTaskFailure rather than the retry branch.
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at, attempt, max_attempts)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours', 2, 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create stale task: %v", err)
	}

	queries := db.New(testPool)
	bus := events.New()

	// Fail the stale task (running timeout of 1 second — our task is 3 hours old).
	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	// Confirm our task was swept.
	found := false
	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task %s to be in failed tasks, got %v", taskID, failedTasks)
	}

	// This is what we're testing: issue must be reset from in_progress → todo
	// when no retry is scheduled. The fixture sets attempt=max_attempts so the
	// retry guard rejects, leaving HandleTaskFailure to run the reset branch.
	handleSweptFailures(ctx, queries, newSweepTaskSvc(queries, bus), failedTasks, "task timed out")

	var issueStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus)
	if err != nil {
		t.Fatalf("failed to query issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("expected issue status 'todo' after sweep, got '%s' — issue is stuck", issueStatus)
	}
}

// TestSweepDoesNotResetIssueAlreadyInReview verifies that the sweeper only resets
// issues that are truly stuck in in_progress — it must not clobber issues whose
// agents already moved them forward (e.g. to in_review) before the task timed out.
func TestSweepDoesNotResetIssueAlreadyInReview(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Issue already advanced to in_review by the agent before the task timed out.
	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Already in_review issue', 'in_review', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create stale task: %v", err)
	}

	queries := db.New(testPool)
	bus := events.New()

	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	handleSweptFailures(ctx, queries, newSweepTaskSvc(queries, bus), failedTasks, "task timed out")

	// Issue should remain in_review — the sweeper must not clobber agent progress.
	var issueStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus)
	if err != nil {
		t.Fatalf("failed to query issue status: %v", err)
	}
	if issueStatus != "in_review" {
		t.Fatalf("expected issue status 'in_review' to be preserved, got '%s'", issueStatus)
	}
}

// TestSweepSchedulesRetryForTransientFailure verifies that when the sweeper
// fails a task with a transient class (timeout, runtime offline) and the
// task still has retry budget, a fresh queued task is enqueued for the
// same (issue, agent) pair and a task:retry_scheduled event fires.
func TestSweepSchedulesRetryForTransientFailure(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, parentID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	bus := events.New()

	var retryEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("task:retry_scheduled", func(e events.Event) {
		mu.Lock()
		retryEvents = append(retryEvents, e)
		mu.Unlock()
	})

	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	handleSweptFailures(context.Background(), queries, newSweepTaskSvc(queries, bus), failedTasks, "task timed out")

	// Parent should still be 'failed', a fresh 'queued' child must exist for the same issue/agent.
	var queuedCount int
	err = testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND parent_task_id = $3
	`, issueID, agentID, parentID).Scan(&queuedCount)
	if err != nil {
		t.Fatalf("count retry tasks: %v", err)
	}
	if queuedCount != 1 {
		t.Fatalf("expected exactly 1 retry task chained to parent %s, got %d", parentID, queuedCount)
	}

	// task:retry_scheduled must have fired with parent_task_id = parentID and attempt=2.
	mu.Lock()
	defer mu.Unlock()
	var found bool
	for _, e := range retryEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["parent_task_id"] == parentID {
			if attempt, _ := payload["attempt"].(int32); attempt != 2 {
				t.Fatalf("expected retry attempt=2, got %v", payload["attempt"])
			}
			if e.WorkspaceID == "" {
				t.Fatal("task:retry_scheduled event missing WorkspaceID")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task:retry_scheduled for parent %s, got events: %v", parentID, retryEvents)
	}
}

// parseUUIDBytes converts a UUID string to the 16-byte array used by pgtype.UUID.
func parseUUIDBytes(s string) [16]byte {
	s = strings.ReplaceAll(s, "-", "")
	var b [16]byte
	for i := 0; i < 16; i++ {
		hi := unhex(s[i*2])
		lo := unhex(s[i*2+1])
		b[i] = hi<<4 | lo
	}
	return b
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}
