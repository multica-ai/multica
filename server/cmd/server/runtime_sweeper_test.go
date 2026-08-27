package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRuntimeGCRunsOutsideTheLivenessLoop pins PR1's deployment boundary: GC
// keeps its existing predicates and budgets, but its seven-day retention scan
// no longer occupies the 30-second runtime/task loop.
func TestRuntimeGCRunsOutsideTheLivenessLoop(t *testing.T) {
	if runtimeGCSweepInterval != time.Hour {
		t.Fatalf("runtime GC interval = %s, want 1h", runtimeGCSweepInterval)
	}

	source, err := os.ReadFile("runtime_sweeper.go")
	if err != nil {
		t.Fatalf("read runtime_sweeper.go: %v", err)
	}
	start := strings.Index(string(source), "func runRuntimeSweeper(")
	end := strings.Index(string(source), "func runRuntimeGCSweeper(")
	if start < 0 || end <= start {
		t.Fatal("could not isolate runtime sweeper loops")
	}
	if strings.Contains(string(source[start:end]), "gcRuntimes(") {
		t.Fatal("runtime GC is still invoked from the 30-second liveness loop")
	}
}

// TestRuntimeGCDailyCandidateCapacity prevents an interval or batch-size change
// from silently reducing the hourly GC worker below its agreed daily capacity.
func TestRuntimeGCDailyCandidateCapacity(t *testing.T) {
	const wantCandidatesPerDay = 12_000
	got := int(24*time.Hour/runtimeGCSweepInterval) * runtimeGCBatchSize
	if got != wantCandidatesPerDay {
		t.Fatalf("runtime GC candidate capacity = %d/day, want %d/day", got, wantCandidatesPerDay)
	}
}

func TestPeriodicSweepStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{}, 1)
	stopped := make(chan struct{})
	go func() {
		runPeriodicSweep(ctx, 10*time.Millisecond, func() {
			select {
			case called <- struct{}{}:
			default:
			}
		})
		close(stopped)
	}()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic sweep did not run")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic sweep did not stop with its context")
	}
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

// ageOutAgentRuntime marks the agent's runtime as stale — old last_seen_at —
// so the runtime-liveness gate on the running-task sweep predicate
// (agent_runtime.last_seen_at within staleThresholdSeconds) does NOT protect
// the test task from being killed by the wall clock. Register a cleanup that
// restores last_seen_at so subsequent tests re-using this runtime see it as
// fresh. Callers pass a `staleAgo` well beyond staleThresholdSeconds so tests
// are insensitive to that constant's precise value.
func ageOutAgentRuntime(t *testing.T, agentID string, staleAgo time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime SET last_seen_at = now() - make_interval(secs => $1)
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $2)
	`, staleAgo.Seconds(), agentID); err != nil {
		t.Fatalf("failed to age out agent runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET last_seen_at = now()
			WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
		`, agentID)
	})
}

func setAgentRuntimeOffline(t *testing.T, agentID string, lastSeenAgo time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime
		SET status = 'offline', last_seen_at = now() - make_interval(secs => $1)
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $2)
	`, lastSeenAgo.Seconds(), agentID); err != nil {
		t.Fatalf("failed to mark agent runtime offline: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET status = 'online', last_seen_at = now()
			WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
		`, agentID)
	})
}

func TestRefreshAgentStatusFromTasks(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	taskService := service.NewTaskService(queries, testPool, nil, events.New())

	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed idle agent status: %v", err)
	}

	agent, err := queries.RefreshAgentStatusFromTasks(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("RefreshAgentStatusFromTasks with dispatched task failed: %v", err)
	}
	if agent.Status != "working" {
		t.Fatalf("expected dispatched task to refresh agent status to working, got %q", agent.Status)
	}

	if _, err := taskService.MarkTaskWaitingLocalDirectory(ctx, parseUUID(taskID), "test path busy"); err != nil {
		t.Fatalf("MarkTaskWaitingLocalDirectory failed: %v", err)
	}
	agent, err = queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("GetAgent after local-directory wait failed: %v", err)
	}
	if agent.Status != "idle" {
		t.Fatalf("expected waiter-only agent status idle, got %q", agent.Status)
	}

	if _, err := taskService.StartTask(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("StartTask from local-directory wait failed: %v", err)
	}
	agent, err = queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("GetAgent after StartTask failed: %v", err)
	}
	if agent.Status != "working" {
		t.Fatalf("expected running task to restore agent status working, got %q", agent.Status)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'cancelled', completed_at = now()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("failed to cancel seeded task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to reseed working agent status: %v", err)
	}

	agent, err = queries.RefreshAgentStatusFromTasks(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("RefreshAgentStatusFromTasks with no active tasks failed: %v", err)
	}
	if agent.Status != "idle" {
		t.Fatalf("expected cancelled-only task set to refresh agent status to idle, got %q", agent.Status)
	}
}

func TestStartTaskSkipsUnchangedAgentStatusWriteAndBroadcast(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	var updatedAtBefore time.Time
	if err := testPool.QueryRow(ctx, `
		UPDATE agent
		SET status = 'working', updated_at = now() - interval '1 hour'
		WHERE id = $1
		RETURNING updated_at
	`, agentID).Scan(&updatedAtBefore); err != nil {
		t.Fatalf("seed working agent status: %v", err)
	}

	bus := events.New()
	statusEvents := 0
	bus.Subscribe("agent:status", func(events.Event) {
		statusEvents++
	})
	taskService := service.NewTaskService(db.New(testPool), testPool, nil, bus)

	if _, err := taskService.StartTask(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("StartTask from dispatched failed: %v", err)
	}

	var (
		status         string
		updatedAtAfter time.Time
	)
	if err := testPool.QueryRow(ctx, `
		SELECT status, updated_at FROM agent WHERE id = $1
	`, agentID).Scan(&status, &updatedAtAfter); err != nil {
		t.Fatalf("load agent after StartTask: %v", err)
	}
	if status != "working" {
		t.Fatalf("agent status after StartTask = %q, want working", status)
	}
	if !updatedAtAfter.Equal(updatedAtBefore) {
		t.Fatalf("unchanged status rewrote updated_at: before=%s after=%s", updatedAtBefore, updatedAtAfter)
	}
	if statusEvents != 0 {
		t.Fatalf("unchanged status broadcasts = %d, want 0", statusEvents)
	}
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
	// The running-task sweep now requires the task's runtime to be NOT
	// heartbeating (MUL-4107). Age the runtime out so this test still
	// exercises the sweeper wall clock rather than being silently skipped.
	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

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
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0, // 1 second — our task is 3 hours old
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
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

	// Call broadcastFailedTasks — this is what we're testing
	broadcastFailedTasks(context.Background(), queries, nil, bus, failedTasks)

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
			if e.TaskID != taskID {
				t.Fatalf("expected envelope TaskID %s, got %s", taskID, e.TaskID)
			}
			if payload["error"] != "task timed out" {
				t.Fatalf("expected deliverable error %q, got %v", "task timed out", payload["error"])
			}
			if payload["failure_reason"] != "timeout" || payload["retry_pending"] != false {
				t.Fatalf("unexpected failure metadata: reason=%v retry_pending=%v", payload["failure_reason"], payload["retry_pending"])
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
	// Runtime must be stale for the running-task wall clock to fire (MUL-4107).
	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

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
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale task")
	}

	broadcastFailedTasks(context.Background(), queries, nil, bus, failedTasks)

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
		// RuntimeStaleSecs only affects the running branch — irrelevant for
		// this dispatched-timeout test, but wired for API consistency.
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale dispatched task")
	}

	broadcastFailedTasks(context.Background(), queries, nil, bus, failedTasks)

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

// TestSweepDispatchedTaskWaitsThroughReconnectGrace locks the network-partition
// behavior for the pre-start window: an expired prepare lease must not consume
// a retry while the runtime has only recently gone offline.
func TestSweepDispatchedTaskWaitsThroughReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)

	failedTasks, err := db.New(testPool).FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       1.0,
		RunningTimeoutSecs:        9000.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	for _, task := range failedTasks {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatal("dispatched task was failed inside reconnect grace")
		}
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "dispatched" {
		t.Fatalf("task status = %q, want dispatched", status)
	}
}

// TestSweepRunningTaskSkippedWhenRuntimeFresh is the MUL-4107 regression test:
// a running task whose wall-clock deadline has already passed MUST NOT be
// killed by the sweeper as long as its owning runtime is 'online' and its
// last_seen_at is within the runtime stale window. This preserves healthy
// multi-hour research / training runs — the primary motivation for the
// liveness-keyed sweep predicate.
func TestSweepRunningTaskSkippedWhenRuntimeFresh(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	// Runtime heartbeat is fresh (integration fixture inserts last_seen_at=now()).
	// Task started_at is 3h ago; RunningTimeoutSecs=1s would kill on wall clock
	// alone — but the runtime is proving liveness, so the sweeper must skip it.
	queries := db.New(testPool)
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatalf("healthy long-running task on live daemon must NOT be swept — that was the MUL-4107 bug")
		}
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected task to stay 'running', got %q", status)
	}
}

func TestSweepRunningTaskWaitsThroughReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)

	failedTasks, err := db.New(testPool).FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	for _, task := range failedTasks {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatal("long-running task was failed inside reconnect grace")
		}
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "running" {
		t.Fatalf("task status = %q, want running", status)
	}
}

// TestSweepRunningTaskKilledBeyondReconnectGrace is the companion coverage: a
// running task is killed once both its own deadline and the runtime reconnect
// grace have elapsed.
func TestSweepRunningTaskKilledBeyondReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	queries := db.New(testPool)
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	found := false
	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected wall clock to fire when runtime heartbeat is stale, but task %s was not swept", taskID)
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got %q", status)
	}
}

func TestOfflineRuntimeTasksRespectReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)
	queries := db.New(testPool)

	failed, err := queries.FailTasksForOfflineRuntimes(ctx, db.FailTasksForOfflineRuntimesParams{
		ReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
		MaxPerTick:         offlineTaskFailBatchSize,
	})
	if err != nil {
		t.Fatalf("FailTasksForOfflineRuntimes inside grace: %v", err)
	}
	for _, task := range failed {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatal("running task was failed inside reconnect grace")
		}
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime
		SET last_seen_at = now() - make_interval(secs => $1)
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $2)
	`, (defaultRuntimeReconnectGrace + time.Hour).Seconds(), agentID); err != nil {
		t.Fatalf("age runtime beyond grace: %v", err)
	}

	failed, err = queries.FailTasksForOfflineRuntimes(ctx, db.FailTasksForOfflineRuntimesParams{
		ReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
		MaxPerTick:         offlineTaskFailBatchSize,
	})
	if err != nil {
		t.Fatalf("FailTasksForOfflineRuntimes beyond grace: %v", err)
	}
	found := false
	for _, task := range failed {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			if !task.FailureReason.Valid || task.FailureReason.String != "runtime_offline" {
				t.Fatalf("failure reason = %q, want runtime_offline", task.FailureReason.String)
			}
		}
	}
	if !found {
		t.Fatal("task was not failed after reconnect grace elapsed")
	}
}

func TestRuntimeReconnectRetryHasBoundedTerminalPath(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	issueID, agentID, parentID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'failed', completed_at = now(), failure_reason = 'runtime_offline'
		WHERE id = $1
	`, parentID); err != nil {
		t.Fatalf("fail runtime_offline parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("mark issue in progress: %v", err)
	}

	var retryID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, fire_at,
			parent_task_id, retry_of_task_id, attempt, max_attempts
		)
		SELECT agent_id, runtime_id, issue_id, 'deferred', priority,
		       now() - interval '10 minutes', id, id, attempt + 1, max_attempts
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, parentID).Scan(&retryID); err != nil {
		t.Fatalf("insert deferred retry: %v", err)
	}

	queries := db.New(testPool)
	params := db.FailExpiredRuntimeReconnectRetriesParams{
		ReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
		RuntimeStaleSecs:   staleThresholdSeconds,
		MaxPerTick:         reconnectRetryExpireBatchSize,
	}

	failed, err := queries.FailExpiredRuntimeReconnectRetries(ctx, params)
	if err != nil {
		t.Fatalf("expire retry inside grace: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("retry failed inside reconnect grace: got %d rows", len(failed))
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET fire_at = now() - make_interval(secs => $1)
		WHERE id = $2
	`, (defaultRuntimeReconnectGrace + time.Hour).Seconds(), retryID); err != nil {
		t.Fatalf("age retry beyond reconnect grace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime SET status = 'online', last_seen_at = now()
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
	`, agentID); err != nil {
		t.Fatalf("restore healthy runtime: %v", err)
	}

	failed, err = queries.FailExpiredRuntimeReconnectRetries(ctx, params)
	if err != nil {
		t.Fatalf("expire retry after healthy reconnect: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("healthy runtime lost reconnect race: got %d rows", len(failed))
	}

	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)
	failed, err = queries.FailExpiredRuntimeReconnectRetries(ctx, params)
	if err != nil {
		t.Fatalf("expire retry beyond grace: %v", err)
	}
	if len(failed) != 1 || failed[0].ID.Bytes != parseUUIDBytes(retryID) {
		t.Fatalf("expired retries = %d, want retry %s", len(failed), retryID)
	}
	if !failed[0].FailureReason.Valid || failed[0].FailureReason.String != "runtime_reconnect_timeout" {
		t.Fatalf("failure reason = %q, want runtime_reconnect_timeout", failed[0].FailureReason.String)
	}

	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	taskSvc.HandleFailedTasks(ctx, failed)

	var issueStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("issue status = %q, want todo after terminal reconnect timeout", issueStatus)
	}

	var undrained, retryChildren int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE completed_at IS NULL),
		       count(*) FILTER (WHERE parent_task_id = $1)
		FROM agent_task_queue WHERE issue_id = $2
	`, retryID, issueID).Scan(&undrained, &retryChildren); err != nil {
		t.Fatalf("read terminal retry state: %v", err)
	}
	if undrained != 0 || retryChildren != 0 {
		t.Fatalf("terminal retry leaked work: undrained=%d child_retries=%d", undrained, retryChildren)
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

	// Runtime must be stale for the running-task wall clock to fire (MUL-4107).
	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	// Fail the stale task (running timeout of 1 second — our task is 3 hours old).
	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
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

	// This is what we're testing: issue must be reset from in_progress → todo.
	broadcastFailedTasks(ctx, queries, nil, bus, failedTasks)

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

	// Runtime must be stale for the running-task wall clock to fire (MUL-4107).
	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	broadcastFailedTasks(ctx, queries, nil, bus, failedTasks)

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

// TestExpireStaleQueuedTasks verifies the MUL-1899 queued-TTL sweeper:
// tasks that have been sitting in 'queued' beyond the TTL are transitioned
// to 'failed' with failure_reason='queued_expired', while fresh queued tasks
// are left alone and the per-tick batch limit is respected.
func TestExpireStaleQueuedTasks(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	// Find the integration test agent
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// One ancient queued task (should expire) and one fresh queued task (should not).
	// Constraint: idx_one_pending_task_per_issue_agent → use distinct issues.
	mkIssue := func(label string) string {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			WITH bumped AS (
				UPDATE workspace SET issue_counter = issue_counter + 1
				WHERE id = $1 RETURNING issue_counter
			)
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
			SELECT $1, $3, 'todo', 'none', 'member', m.user_id, 'agent', $2, (SELECT issue_counter FROM bumped)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, agentID, label).Scan(&issueID); err != nil {
			t.Fatalf("failed to create %s issue: %v", label, err)
		}
		return issueID
	}
	oldIssueID := mkIssue("Queued TTL test (old)")
	freshIssueID := mkIssue("Queued TTL test (fresh)")
	recoveryIssueID := mkIssue("Queued TTL test (runtime recovery)")
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2, $3)`, oldIssueID, freshIssueID, recoveryIssueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id IN ($1, $2, $3)`, oldIssueID, freshIssueID, recoveryIssueID)
	})

	var oldTaskID, freshTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours')
		RETURNING id
	`, agentID, runtimeID, oldIssueID).Scan(&oldTaskID); err != nil {
		t.Fatalf("failed to insert old queued task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'queued', 0, now())
		RETURNING id
	`, agentID, runtimeID, freshIssueID).Scan(&freshTaskID); err != nil {
		t.Fatalf("failed to insert fresh queued task: %v", err)
	}
	var recoveryParentID, recoveryTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 hours', now() - interval '5 hours', 'runtime_offline')
		RETURNING id
	`, agentID, runtimeID, recoveryIssueID).Scan(&recoveryParentID); err != nil {
		t.Fatalf("failed to insert runtime_offline parent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, parent_task_id, retry_of_task_id)
		VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours', $4, $4)
		RETURNING id
	`, agentID, runtimeID, recoveryIssueID, recoveryParentID).Scan(&recoveryTaskID); err != nil {
		t.Fatalf("failed to insert runtime recovery retry: %v", err)
	}

	queries := db.New(testPool)
	failed, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    3600.0, // 1h TTL — old task is 5h, fresh task is 0s
		MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected exactly 1 expired task, got %d", len(failed))
	}
	if failed[0].ID.Bytes != parseUUIDBytes(oldTaskID) {
		t.Fatalf("expired the wrong task: got %x", failed[0].ID.Bytes)
	}

	// DB assertions: old → failed/queued_expired, fresh → still queued.
	var oldStatus, oldReason, oldErr string
	if err := testPool.QueryRow(ctx, `
		SELECT status, COALESCE(failure_reason, ''), COALESCE(error, '')
		FROM agent_task_queue WHERE id = $1
	`, oldTaskID).Scan(&oldStatus, &oldReason, &oldErr); err != nil {
		t.Fatalf("failed to read old task: %v", err)
	}
	if oldStatus != "failed" {
		t.Fatalf("old task: expected status=failed, got %q", oldStatus)
	}
	if oldReason != "queued_expired" {
		t.Fatalf("old task: expected failure_reason=queued_expired, got %q", oldReason)
	}
	if !strings.Contains(oldErr, "expired in queue") {
		t.Fatalf("old task: expected error to mention expiry, got %q", oldErr)
	}

	var freshStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT status FROM agent_task_queue WHERE id = $1
	`, freshTaskID).Scan(&freshStatus); err != nil {
		t.Fatalf("failed to read fresh task: %v", err)
	}
	if freshStatus != "queued" {
		t.Fatalf("fresh task: expected status=queued, got %q", freshStatus)
	}

	var recoveryStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, recoveryTaskID).Scan(&recoveryStatus); err != nil {
		t.Fatalf("failed to read runtime recovery retry: %v", err)
	}
	if recoveryStatus != "queued" {
		t.Fatalf("runtime recovery retry: expected status=queued, got %q", recoveryStatus)
	}
}

// TestExpireStaleQueuedTasksRespectsBatchLimit verifies the per-tick cap so
// that a large historical backlog cannot monopolise a single sweep.
func TestExpireStaleQueuedTasksRespectsBatchLimit(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create 5 issues, each with one stale queued task — necessary because of the
	// idx_one_pending_task_per_issue_agent unique constraint.
	var issueIDs []string
	t.Cleanup(func() {
		for _, id := range issueIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
	})
	for i := 0; i < 5; i++ {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			WITH bumped AS (
				UPDATE workspace SET issue_counter = issue_counter + 1
				WHERE id = $1 RETURNING issue_counter
			)
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
			SELECT $1, 'Queued TTL batch test', 'todo', 'none', 'member', m.user_id, 'agent', $2, (SELECT issue_counter FROM bumped)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, agentID).Scan(&issueID); err != nil {
			t.Fatalf("failed to create issue %d: %v", i, err)
		}
		issueIDs = append(issueIDs, issueID)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
			VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours')
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("failed to insert backlog task %d: %v", i, err)
		}
	}

	queries := db.New(testPool)
	failed, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    3600.0,
		MaxPerTick: 2, // cap below the backlog
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	if len(failed) != 2 {
		t.Fatalf("expected batch cap of 2, got %d", len(failed))
	}

	var remaining int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE issue_id = ANY($1::uuid[]) AND status = 'queued'
	`, issueIDs).Scan(&remaining); err != nil {
		t.Fatalf("failed to count remaining queued: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("expected 3 queued tasks remaining after batched sweep, got %d", remaining)
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
