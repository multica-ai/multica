package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type queuedExpiredCreateIssueFixture struct {
	agentID     string
	runtimeID   string
	autopilotID string
	runID       string
	issueID     string
	taskID      string
}

func setupQueuedExpiredCreateIssueFixture(t *testing.T) queuedExpiredCreateIssueFixture {
	t.Helper()
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var f queuedExpiredCreateIssueFixture
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id
		FROM agent a
		WHERE a.workspace_id = $1 AND a.runtime_id IS NOT NULL AND a.archived_at IS NULL
		LIMIT 1
	`, testWorkspaceID).Scan(&f.agentID, &f.runtimeID); err != nil {
		t.Fatalf("setup: get agent/runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id, execution_mode,
			created_by_type, created_by_id
		)
		VALUES ($1, 'queued-expired recovery fixture', 'agent', $2, 'create_issue', 'member', $3)
		RETURNING id
	`, testWorkspaceID, f.agentID, testUserID).Scan(&f.autopilotID); err != nil {
		t.Fatalf("setup: create autopilot: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			assignee_type, assignee_id, origin_type, origin_id
		)
		VALUES ($1, 'queued-expired created issue', 'todo', 'none', 'agent', $2,
			'agent', $2, 'autopilot', $3)
		RETURNING id
	`, testWorkspaceID, f.agentID, f.autopilotID).Scan(&f.issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status, issue_id)
		VALUES ($1, 'schedule', 'issue_created', $2)
		RETURNING id
	`, f.autopilotID, f.issueID).Scan(&f.runID); err != nil {
		t.Fatalf("setup: create autopilot run: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, created_at,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'queued', 0, now() - interval '3 hours', 1, 2)
		RETURNING id
	`, f.agentID, f.runtimeID, f.issueID).Scan(&f.taskID); err != nil {
		t.Fatalf("setup: create failed task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, f.issueID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, f.autopilotID)
	})
	return f
}

func expireQueuedFixture(t *testing.T, taskSvc *service.TaskService, queries *db.Queries, taskID string) db.AgentTaskQueue {
	t.Helper()
	sweepExpiredQueuedTasks(context.Background(), queries, taskSvc)
	task, err := queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatalf("load expired task: %v", err)
	}
	if task.Status != "failed" || !task.FailureReason.Valid || task.FailureReason.String != "queued_expired" {
		t.Fatalf("expired task = status %q reason %#v", task.Status, task.FailureReason)
	}
	return task
}

func TestHandleFailedTasksSurfacesQueuedExpiredCreateIssue(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	var status string
	var metadataJSON []byte
	if err := testPool.QueryRow(ctx, `SELECT status, metadata FROM issue WHERE id = $1`, f.issueID).Scan(&status, &metadataJSON); err != nil {
		t.Fatalf("read surfaced issue: %v", err)
	}
	if status != "blocked" {
		t.Fatalf("issue status = %q, want blocked", status)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["pipeline_status"] != "queued_expired" {
		t.Fatalf("pipeline_status = %#v, want queued_expired", metadata["pipeline_status"])
	}
	if metadata["waiting_on"] != "runtime_reconnect" {
		t.Fatalf("waiting_on = %#v, want runtime_reconnect", metadata["waiting_on"])
	}

	var comments int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND type = 'system' AND source_task_id = $2
	`, f.issueID, f.taskID).Scan(&comments); err != nil {
		t.Fatalf("count expiry comments: %v", err)
	}
	if comments != 1 {
		t.Fatalf("expiry system comments = %d, want 1", comments)
	}
}

func TestRecoverQueuedExpiredCreateIssueOnRuntimeReconnect(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("recover on reconnect: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	var status string
	var metadataJSON []byte
	if err := testPool.QueryRow(ctx, `SELECT status, metadata FROM issue WHERE id = $1`, f.issueID).Scan(&status, &metadataJSON); err != nil {
		t.Fatalf("read recovered issue: %v", err)
	}
	if status != "todo" {
		t.Fatalf("issue status = %q, want todo", status)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["pipeline_status"] != "recovery_queued" {
		t.Fatalf("pipeline_status = %#v, want recovery_queued", metadata["pipeline_status"])
	}
	if metadata["waiting_on"] != "runtime_execution" {
		t.Fatalf("waiting_on = %#v, want runtime_execution", metadata["waiting_on"])
	}

	var parentID *string
	var attempt, maxAttempts int
	if err := testPool.QueryRow(ctx, `
		SELECT parent_task_id::text, attempt, max_attempts
		FROM agent_task_queue
		WHERE issue_id = $1 AND id <> $2
	`, f.issueID, f.taskID).Scan(&parentID, &attempt, &maxAttempts); err != nil {
		t.Fatalf("read recovery task: %v", err)
	}
	if parentID == nil || *parentID != f.taskID {
		t.Fatalf("recovery parent = %#v, want %s", parentID, f.taskID)
	}
	if attempt != 2 || maxAttempts != 2 {
		t.Fatalf("recovery budget = %d/%d, want 2/2", attempt, maxAttempts)
	}
}

func TestRecoverQueuedExpiredCreateIssueDeduplicatesReconnects(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	first, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("first reconnect: %v", err)
	}
	second, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("second reconnect: %v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("recovered counts = %d then %d, want 1 then 0", first, second)
	}
	var children int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, f.taskID).Scan(&children); err != nil {
		t.Fatalf("count recovery children: %v", err)
	}
	if children != 1 {
		t.Fatalf("recovery children = %d, want 1", children)
	}
}

func TestRecoverQueuedExpiredCreateIssueWaitsForDurableSurface(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())

	failed, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs: 3600, MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("expire queued task: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != parseUUID(f.taskID) {
		t.Fatalf("expired tasks = %#v, want fixture task", failed)
	}

	// A heartbeat can land after the task failure commits but before failure
	// side effects run. Recovery must wait instead of racing the durable block.
	if recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID)); err != nil || recovered != 0 {
		t.Fatalf("pre-surface recovery = %d, %v; want 0, nil", recovered, err)
	}
	taskSvc.HandleFailedTasks(ctx, failed)
	if recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID)); err != nil || recovered != 1 {
		t.Fatalf("post-surface recovery = %d, %v; want 1, nil", recovered, err)
	}

	var status, pipelineStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT status, metadata ->> 'pipeline_status' FROM issue WHERE id = $1
	`, f.issueID).Scan(&status, &pipelineStatus); err != nil {
		t.Fatalf("read final issue state: %v", err)
	}
	if status != "todo" || pipelineStatus != "recovery_queued" {
		t.Fatalf("final issue state = %s/%s, want todo/recovery_queued", status, pipelineStatus)
	}
}

func TestQueuedExpiredSurfaceRechecksAfterConcurrentEnqueue(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	failed, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs: 3600, MaxPerTick: 100,
	})
	if err != nil || len(failed) != 1 {
		t.Fatalf("expire fixture = %d tasks, %v; want 1, nil", len(failed), err)
	}

	// Model an ordinary enqueue transaction that owns the shared issue lock and
	// commits a later task while surface is waiting. Surface must take a fresh
	// snapshot after acquiring the lock and observe the insert.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin enqueue transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM issue WHERE id = $1 FOR UPDATE`, f.issueID); err != nil {
		t.Fatalf("lock issue: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, f.agentID, f.runtimeID, f.issueID); err != nil {
		t.Fatalf("insert concurrent task: %v", err)
	}

	done := make(chan struct{})
	go func() {
		taskSvc.HandleFailedTasks(ctx, failed)
		close(done)
	}()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent enqueue: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("surface remained blocked after enqueue commit")
	}

	var status string
	var metadataJSON []byte
	if err := testPool.QueryRow(ctx, `SELECT status, metadata FROM issue WHERE id = $1`, f.issueID).Scan(&status, &metadataJSON); err != nil {
		t.Fatalf("read issue: %v", err)
	}
	if status != "todo" || string(metadataJSON) != "{}" {
		t.Fatalf("surface overwrote concurrent enqueue: status=%q metadata=%s", status, metadataJSON)
	}
}

func TestRecoverQueuedExpiredCreateIssueSkipsAlreadyWorkedIssue(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)
	expireQueuedFixture(t, taskSvc, queries, f.taskID)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, started_at, completed_at
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now())
	`, f.agentID, f.runtimeID, f.issueID); err != nil {
		t.Fatalf("seed later completed work: %v", err)
	}

	recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("recover already-worked issue: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered = %d, want 0", recovered)
	}
}

func TestRecoverQueuedExpiredCreateIssueHonorsMaxAttempts(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET max_attempts = 1 WHERE id = $1`, f.taskID); err != nil {
		t.Fatalf("exhaust retry budget: %v", err)
	}
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("recover exhausted task: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered = %d, want 0", recovered)
	}
}

func TestManualRerunSupersedesQueuedExpiredRecovery(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	manual, err := taskSvc.RerunIssue(ctx, parseUUID(f.issueID), parseUUID(f.taskID), optionalUUID(""), parseUUID(testUserID), nil)
	if err != nil {
		t.Fatalf("manual rerun: %v", err)
	}
	if !manual.ForceFreshSession {
		t.Fatal("manual rerun force_fresh_session = false, want true")
	}
	if recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID)); err != nil || recovered != 0 {
		t.Fatalf("recovery after manual rerun = %d, %v; want 0, nil", recovered, err)
	}

	var taskCount int
	var status, pipelineStatus, waitingOn string
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, f.issueID).Scan(&taskCount); err != nil {
		t.Fatalf("count issue tasks: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT status, metadata ->> 'pipeline_status', metadata ->> 'waiting_on'
		FROM issue WHERE id = $1
	`, f.issueID).Scan(&status, &pipelineStatus, &waitingOn); err != nil {
		t.Fatalf("read pipeline status: %v", err)
	}
	if taskCount != 2 || status != "todo" || pipelineStatus != "manual_rerun" || waitingOn != "runtime_execution" {
		t.Fatalf("manual state = tasks:%d %s/%s/%s, want 2 todo/manual_rerun/runtime_execution", taskCount, status, pipelineStatus, waitingOn)
	}
}

func TestNewIssueWorkSupersedesQueuedExpiredRecovery(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	issue, err := queries.GetIssue(ctx, parseUUID(f.issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if _, err := taskSvc.EnqueueTaskForMention(ctx, issue, parseUUID(f.agentID), optionalUUID("")); err != nil {
		t.Fatalf("enqueue new issue work: %v", err)
	}
	if recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID)); err != nil || recovered != 0 {
		t.Fatalf("recovery after new work = %d, %v; want 0, nil", recovered, err)
	}

	var status, pipelineStatus, waitingOn string
	if err := testPool.QueryRow(ctx, `
		SELECT status, metadata ->> 'pipeline_status', metadata ->> 'waiting_on'
		FROM issue WHERE id = $1
	`, f.issueID).Scan(&status, &pipelineStatus, &waitingOn); err != nil {
		t.Fatalf("read issue state: %v", err)
	}
	if status != "todo" || pipelineStatus != "new_work_queued" || waitingOn != "runtime_execution" {
		t.Fatalf("new-work state = %s/%s/%s, want todo/new_work_queued/runtime_execution", status, pipelineStatus, waitingOn)
	}
}

func TestQueuedExpiredNonCreateIssueKeepsExistingBehavior(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM autopilot_run WHERE id = $1`, f.runID); err != nil {
		t.Fatalf("remove create_issue run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE issue SET origin_type = NULL, origin_id = NULL WHERE id = $1`, f.issueID); err != nil {
		t.Fatalf("convert to ordinary issue: %v", err)
	}
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	var status string
	var metadataJSON []byte
	if err := testPool.QueryRow(ctx, `SELECT status, metadata FROM issue WHERE id = $1`, f.issueID).Scan(&status, &metadataJSON); err != nil {
		t.Fatalf("read ordinary issue: %v", err)
	}
	if status != "todo" || string(metadataJSON) != "{}" {
		t.Fatalf("ordinary issue changed: status=%q metadata=%s", status, metadataJSON)
	}
	recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("recover ordinary issue: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("ordinary issue recovered = %d, want 0", recovered)
	}
}

func TestQueuedExpiredCreateIssueIgnoresTerminalAutopilotRun(t *testing.T) {
	f := setupQueuedExpiredCreateIssueFixture(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_run SET status = 'completed', completed_at = now() WHERE id = $1
	`, f.runID); err != nil {
		t.Fatalf("complete autopilot run: %v", err)
	}
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	expireQueuedFixture(t, taskSvc, queries, f.taskID)

	var status string
	var metadataJSON []byte
	if err := testPool.QueryRow(ctx, `SELECT status, metadata FROM issue WHERE id = $1`, f.issueID).Scan(&status, &metadataJSON); err != nil {
		t.Fatalf("read terminal-run issue: %v", err)
	}
	if status != "todo" || string(metadataJSON) != "{}" {
		t.Fatalf("terminal-run issue changed: status=%q metadata=%s", status, metadataJSON)
	}
	if recovered, err := taskSvc.RecoverQueuedExpiredCreateIssueTasksForRuntime(ctx, parseUUID(f.runtimeID)); err != nil || recovered != 0 {
		t.Fatalf("terminal-run recovery = %d, %v; want 0, nil", recovered, err)
	}
}
