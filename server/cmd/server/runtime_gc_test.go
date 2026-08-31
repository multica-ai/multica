package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeGC_KeepsTerminalTaskHistory(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "history")
	agentID := createRuntimeGCFixtureAgent(t, ctx, runtimeID, "history")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive fixture agent: %v", err)
	}

	tasks := make([]string, 0, 3)
	for _, status := range []string{"completed", "failed", "cancelled"} {
		tasks = append(tasks, createRuntimeGCFixtureTask(t, ctx, runtimeID, agentID, status, true))
	}
	taskID := tasks[0]
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'assistant', 'runtime GC must preserve this transcript')
	`, taskID); err != nil {
		t.Fatalf("insert task message: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
		VALUES ($1, 'test', 'runtime-gc', 10, 20)
	`, taskID); err != nil {
		t.Fatalf("insert task usage: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour')
	`, "runtime-gc-"+taskID, taskID, agentID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("insert task token: %v", err)
	}

	result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if !result.deleted || result.skipReason != "" {
		t.Fatalf("gc result deleted=%v skip_reason=%q, want deleted", result.deleted, result.skipReason)
	}
	if result.workspaceID != testWorkspaceID {
		t.Fatalf("workspace id = %q, want %q", result.workspaceID, testWorkspaceID)
	}
	if len(result.teardown.CancelledTasks) != 0 {
		t.Fatalf("automatic GC cancelled %d tasks, want none", len(result.teardown.CancelledTasks))
	}

	var runtimeRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if runtimeRows != 0 {
		t.Fatalf("runtime rows = %d, want 0", runtimeRows)
	}
	var survivingAgents int
	var agentUnbound bool
	if err := testPool.QueryRow(ctx, `
		SELECT count(*), bool_and(runtime_id IS NULL)
		FROM agent WHERE id = $1
	`, agentID).Scan(&survivingAgents, &agentUnbound); err != nil {
		t.Fatalf("read archived agent: %v", err)
	}
	if survivingAgents != 1 || !agentUnbound {
		t.Fatalf("archived agent rows=%d unbound=%v, want preserved and unbound", survivingAgents, agentUnbound)
	}
	for _, id := range tasks {
		var taskRows int
		var runtimeDetached bool
		if err := testPool.QueryRow(ctx, `
			SELECT count(*), bool_and(runtime_id IS NULL)
			FROM agent_task_queue WHERE id = $1
		`, id).Scan(&taskRows, &runtimeDetached); err != nil {
			t.Fatalf("read task %s: %v", id, err)
		}
		if taskRows != 1 || !runtimeDetached {
			t.Fatalf("task %s rows=%d detached=%v, want preserved and detached", id, taskRows, runtimeDetached)
		}
	}
	for table, want := range map[string]int{"task_message": 1, "task_usage": 1, "task_token": 1} {
		var rows int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE task_id = $1`, taskID).Scan(&rows); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if rows != want {
			t.Fatalf("%s rows = %d, want %d", table, rows, want)
		}
	}
	repeated, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil || repeated.deleted || repeated.skipReason != "" {
		t.Fatalf("repeated GC result deleted=%v skip_reason=%q err=%v, want idempotent no-op", repeated.deleted, repeated.skipReason, err)
	}
}

func TestRuntimeGC_ProtectsEveryNonTerminalTaskStatus(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	agentID := runtimeGCFixtureAgentID(t, ctx)

	for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory", "deferred"} {
		t.Run(status, func(t *testing.T) {
			runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "blocked-"+status)
			taskID := createRuntimeGCFixtureTask(t, ctx, runtimeID, agentID, status, false)

			result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
			if err != nil {
				t.Fatalf("gcRuntime: %v", err)
			}
			if result.deleted || result.skipReason != "non_terminal_task" {
				t.Fatalf("gc result deleted=%v skip_reason=%q, want safely task-blocked", result.deleted, result.skipReason)
			}

			var runtimeRows, taskRows int
			var taskRuntime string
			if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
				t.Fatalf("count runtime: %v", err)
			}
			if err := testPool.QueryRow(ctx, `
				SELECT count(*), min(runtime_id::text) FROM agent_task_queue WHERE id = $1
			`, taskID).Scan(&taskRows, &taskRuntime); err != nil {
				t.Fatalf("read task: %v", err)
			}
			if runtimeRows != 1 || taskRows != 1 || taskRuntime != runtimeID {
				t.Fatalf("runtime rows=%d task rows=%d task runtime=%q; GC changed blocked data", runtimeRows, taskRows, taskRuntime)
			}
		})
	}
}

func TestRuntimeGCCandidates_ExcludeBlockedRuntimesWithoutHidingBacklog(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	agentID := runtimeGCFixtureAgentID(t, ctx)
	drainableRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-drainable")
	blockedRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-blocked")
	activeRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-active")
	_ = createRuntimeGCFixtureAgent(t, ctx, activeRuntimeID, "candidate-active")
	archivedRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-archived")
	archivedAgentID := createRuntimeGCFixtureAgent(t, ctx, archivedRuntimeID, "candidate-archived")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, archivedAgentID); err != nil {
		t.Fatalf("archive candidate agent: %v", err)
	}
	systemRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-system")
	systemAgentID := createRuntimeGCFixtureAgent(t, ctx, systemRuntimeID, "candidate-system")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET kind = 'system', system_key = $2 WHERE id = $1`, systemAgentID, "runtime_gc_candidate_"+systemAgentID); err != nil {
		t.Fatalf("convert candidate to system agent: %v", err)
	}
	foreignWorkspaceID := createRuntimeGCFixtureWorkspace(t, ctx, "candidate-mismatch")
	mismatchRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-mismatch")
	mismatchAgentID := createRuntimeGCFixtureAgentForWorkspace(t, ctx, foreignWorkspaceID, mismatchRuntimeID, "candidate-mismatch")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, mismatchAgentID); err != nil {
		t.Fatalf("archive candidate workspace-mismatch agent: %v", err)
	}
	_ = createRuntimeGCFixtureTask(t, ctx, blockedRuntimeID, agentID, "deferred", false)
	queries := db.New(testPool)

	candidates, err := queries.ListStaleOfflineRuntimeGCCandidates(ctx, db.ListStaleOfflineRuntimeGCCandidatesParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxPerTick:   runtimeGCBatchSize,
	})
	if err != nil {
		t.Fatalf("list GC candidates: %v", err)
	}
	foundDrainable, foundArchived, foundSystem := false, false, false
	for _, id := range candidates {
		idString := util.UUIDToString(id)
		if idString == blockedRuntimeID {
			t.Fatal("non-terminal task owner entered the bounded GC batch")
		}
		if idString == activeRuntimeID {
			t.Fatal("runtime with an active user agent entered the bounded GC batch")
		}
		if idString == drainableRuntimeID {
			foundDrainable = true
		}
		if idString == archivedRuntimeID {
			foundArchived = true
		}
		if idString == systemRuntimeID {
			foundSystem = true
		}
	}
	if !foundDrainable {
		t.Fatal("drainable runtime missing from GC candidates")
	}
	if !foundArchived || !foundSystem {
		t.Fatalf("archived/system candidates found=%v/%v, want both", foundArchived, foundSystem)
	}
	backlog, err := queries.CountStaleOfflineRuntimeGCBacklogByReason(ctx, db.CountStaleOfflineRuntimeGCBacklogByReasonParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxRows:      runtimeGCBacklogScanLimit,
	})
	if err != nil {
		t.Fatalf("classify runtime GC backlog: %v", err)
	}
	var activeBlocked, taskBlocked, workspaceMismatch int64
	for _, row := range backlog {
		switch row.Reason {
		case "active_agent":
			activeBlocked = row.Count
		case "non_terminal_task":
			taskBlocked = row.Count
		case "workspace_mismatch":
			workspaceMismatch = row.Count
		}
	}
	if activeBlocked < 1 {
		t.Fatalf("active-agent backlog = %d, want at least 1", activeBlocked)
	}
	if taskBlocked < 1 {
		t.Fatalf("non-terminal-task backlog = %d, want at least 1", taskBlocked)
	}
	if workspaceMismatch < 1 {
		t.Fatalf("workspace-mismatch backlog = %d, want at least 1", workspaceMismatch)
	}
	legacyBlocked, err := queries.CountStaleOfflineRuntimesBlockedByTasks(ctx, db.CountStaleOfflineRuntimesBlockedByTasksParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxRows:      runtimeGCBacklogScanLimit,
	})
	if err != nil {
		t.Fatalf("count legacy task-blocked runtimes: %v", err)
	}

	// The second task predicate must remain independently indexable: a task can
	// be pinned to an online runtime after a historical move while its archived
	// owner is still bound to this stale runtime. Only the agent-id path should
	// classify the stale runtime as task-blocked.
	pinnedOwnerRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-agent-task-owner")
	pinnedAgentID := createRuntimeGCFixtureAgent(t, ctx, pinnedOwnerRuntimeID, "candidate-agent-task-owner")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, pinnedAgentID); err != nil {
		t.Fatalf("archive candidate agent-task owner: %v", err)
	}
	onlineTaskRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "candidate-agent-task-target")
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, onlineTaskRuntimeID); err != nil {
		t.Fatalf("mark task target runtime online: %v", err)
	}
	_ = createRuntimeGCFixtureTask(t, ctx, onlineTaskRuntimeID, pinnedAgentID, "deferred", false)

	afterPinnedTask, err := queries.CountStaleOfflineRuntimeGCBacklogByReason(ctx, db.CountStaleOfflineRuntimeGCBacklogByReasonParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxRows:      runtimeGCBacklogScanLimit,
	})
	if err != nil {
		t.Fatalf("classify runtime GC backlog after pinned agent task: %v", err)
	}
	var taskBlockedAfter int64
	for _, row := range afterPinnedTask {
		if row.Reason == "non_terminal_task" {
			taskBlockedAfter = row.Count
		}
	}
	if taskBlockedAfter != taskBlocked+1 {
		t.Fatalf("non-terminal-task backlog after agent-owned pinned task = %d, want %d", taskBlockedAfter, taskBlocked+1)
	}
	legacyBlockedAfter, err := queries.CountStaleOfflineRuntimesBlockedByTasks(ctx, db.CountStaleOfflineRuntimesBlockedByTasksParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxRows:      runtimeGCBacklogScanLimit,
	})
	if err != nil {
		t.Fatalf("recount legacy task-blocked runtimes: %v", err)
	}
	if legacyBlockedAfter != legacyBlocked {
		t.Fatalf("legacy blocked count changed from %d to %d for the broader agent-owned task case", legacyBlocked, legacyBlockedAfter)
	}
}

func TestRuntimeGC_ArchivedAgentTaskPinnedToAnotherRuntimeFailsClosed(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	staleRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "cross-runtime-owner")
	otherRuntimeID := createRuntimeGCFixtureRuntime(t, ctx, "cross-runtime-task")
	agentID := createRuntimeGCFixtureAgent(t, ctx, staleRuntimeID, "cross-runtime")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive fixture agent: %v", err)
	}
	taskID := createRuntimeGCFixtureTask(t, ctx, otherRuntimeID, agentID, "queued", false)

	result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(staleRuntimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if result.deleted || result.skipReason != "non_terminal_task" {
		t.Fatalf("gc result deleted=%v skip_reason=%q, want non-terminal-task skip", result.deleted, result.skipReason)
	}

	var agentRuntime, taskRuntime, taskStatus string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&agentRuntime); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text, status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskRuntime, &taskStatus); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if agentRuntime != staleRuntimeID || taskRuntime != otherRuntimeID || taskStatus != "queued" {
		t.Fatalf("GC mutated blocked data: agent runtime=%q task runtime=%q status=%q", agentRuntime, taskRuntime, taskStatus)
	}
}

func TestRuntimeGC_RechecksRuntimeEligibilityUnderLock(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "eligibility-changed")
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("bring fixture runtime online: %v", err)
	}

	result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if result.deleted || result.skipReason != "eligibility_changed" {
		t.Fatalf("gc result deleted=%v skip_reason=%q, want eligibility-changed skip", result.deleted, result.skipReason)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&status); err != nil {
		t.Fatalf("read runtime: %v", err)
	}
	if status != "online" {
		t.Fatalf("runtime status=%q, want online", status)
	}
}

func TestRuntimeGC_DeletesSystemAgentAndRuntime(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "system-agent")
	agentID := createRuntimeGCFixtureAgent(t, ctx, runtimeID, "system-agent")
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET kind = 'system', system_key = $2 WHERE id = $1
	`, agentID, "runtime_gc_probe_"+agentID); err != nil {
		t.Fatalf("convert fixture to system agent: %v", err)
	}

	result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if !result.deleted || result.skipReason != "" {
		t.Fatalf("gc result deleted=%v skip_reason=%q, want deleted", result.deleted, result.skipReason)
	}
	var runtimeRows, agentRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent WHERE id = $1`, agentID).Scan(&agentRows); err != nil {
		t.Fatalf("count system agent: %v", err)
	}
	if runtimeRows != 0 || agentRows != 0 {
		t.Fatalf("runtime rows=%d system agent rows=%d, want both deleted", runtimeRows, agentRows)
	}
}

func TestRuntimeGC_ProfileBackedInstanceKeepsProfile(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	profileID := createRuntimeGCFixtureProfile(t, ctx, "preserved")
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "profile-backed")
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET profile_id = $2 WHERE id = $1`, runtimeID, profileID); err != nil {
		t.Fatalf("bind runtime profile: %v", err)
	}

	result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if !result.deleted || result.skipReason != "" {
		t.Fatalf("gc result deleted=%v skip_reason=%q, want deleted", result.deleted, result.skipReason)
	}
	var profiles int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM runtime_profile WHERE id = $1`, profileID).Scan(&profiles); err != nil {
		t.Fatalf("count runtime profile: %v", err)
	}
	if profiles != 1 {
		t.Fatalf("runtime profile rows=%d, want preserved", profiles)
	}
}

func TestRuntimeGC_CrossWorkspaceAgentReferenceFailsClosed(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	foreignWorkspaceID := createRuntimeGCFixtureWorkspace(t, ctx, "mismatch")
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "workspace-mismatch")
	agentID := createRuntimeGCFixtureAgentForWorkspace(t, ctx, foreignWorkspaceID, runtimeID, "workspace-mismatch")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive cross-workspace agent: %v", err)
	}

	result, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if result.deleted || result.skipReason != "workspace_mismatch" {
		t.Fatalf("gc result deleted=%v skip_reason=%q, want workspace-mismatch skip", result.deleted, result.skipReason)
	}
	var runtimeRows int
	var agentRuntime string
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&agentRuntime); err != nil {
		t.Fatalf("read cross-workspace agent: %v", err)
	}
	if runtimeRows != 1 || agentRuntime != runtimeID {
		t.Fatalf("workspace mismatch mutated data: runtime rows=%d agent runtime=%q", runtimeRows, agentRuntime)
	}
}

func TestRuntimeGCBatch_PublishesSystemActorAndDeduplicatesWorkspaceRefresh(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	workspaceID := createRuntimeGCFixtureWorkspace(t, ctx, "events")
	_ = createRuntimeGCFixtureRuntimeForWorkspace(t, ctx, workspaceID, "events-a")
	_ = createRuntimeGCFixtureRuntimeForWorkspace(t, ctx, workspaceID, "events-b")
	publisher := &recordingRuntimeGCPublisher{}

	gcRuntimesWithBudget(ctx, testPool, db.New(testPool), nil, publisher, 5*time.Second)

	var teardownCalls, refreshCalls int
	for _, call := range publisher.calls {
		if call.workspaceID != workspaceID {
			continue
		}
		if call.actorType != "system" || call.actorID != "" || call.action != "runtime_gc" {
			t.Fatalf("unexpected GC event metadata: %+v", call)
		}
		if call.kind == "teardown" {
			teardownCalls++
			if call.publishRuntimeRefresh {
				t.Fatal("per-runtime teardown published an undeduplicated refresh")
			}
			if call.cancelledTasks != 0 {
				t.Fatalf("automatic GC exposed %d cancelled tasks", call.cancelledTasks)
			}
		} else if call.kind == "refresh" {
			refreshCalls++
		}
	}
	if teardownCalls != 2 || refreshCalls != 1 {
		t.Fatalf("workspace GC events: teardown=%d refresh=%d, want 2 and 1", teardownCalls, refreshCalls)
	}
}

func TestRuntimeGC_RollsBackDetachedHistoryWhenDeleteFails(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "rollback")
	agentID := runtimeGCFixtureAgentID(t, ctx)
	taskID := createRuntimeGCFixtureTask(t, ctx, runtimeID, agentID, "completed", true)

	starter := failRuntimeDeleteTxStarter{pool: testPool}
	result, err := gcRuntime(ctx, starter, db.New(testPool), parseUUID(runtimeID))
	if err == nil || !strings.Contains(err.Error(), "injected runtime delete failure") {
		t.Fatalf("gcRuntime error = %v, want injected delete failure", err)
	}
	if result.deleted || result.skipReason != "" {
		t.Fatalf("gc result deleted=%v skip_reason=%q after error", result.deleted, result.skipReason)
	}

	var runtimeRows int
	var taskRuntime string
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskRuntime); err != nil {
		t.Fatalf("read rolled-back task: %v", err)
	}
	if runtimeRows != 1 || taskRuntime != runtimeID {
		t.Fatalf("rollback left runtime rows=%d task runtime=%q", runtimeRows, taskRuntime)
	}
}

func TestRuntimeGC_RuntimeLockRejectsConcurrentEnqueue(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "enqueue-race")
	agentID := runtimeGCFixtureAgentID(t, ctx)

	locked := make(chan struct{})
	proceed := make(chan struct{})
	starter := signalRuntimeLockTxStarter{pool: testPool, locked: locked, proceed: proceed}
	type gcResult struct {
		result runtimeGCResult
		err    error
	}
	gcDone := make(chan gcResult, 1)
	go func() {
		result, err := gcRuntime(ctx, starter, db.New(testPool), parseUUID(runtimeID))
		gcDone <- gcResult{result: result, err: err}
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("runtime GC never acquired its FOR UPDATE lock")
	}

	writer, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire writer connection: %v", err)
	}
	defer writer.Release()
	var writerPID int32
	if err := writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatalf("read writer pid: %v", err)
	}
	writerDone := make(chan error, 1)
	go func() {
		_, err := db.New(writer).CreateAgentTask(ctx, db.CreateAgentTaskParams{
			AgentID:   parseUUID(agentID),
			RuntimeID: parseUUID(runtimeID),
			Priority:  0,
		})
		writerDone <- err
	}()
	waitErr := waitForRuntimeGCBlockedWriter(ctx, writerPID)
	close(proceed)
	if waitErr != nil {
		t.Fatal(waitErr)
	}

	result := <-gcDone
	if result.err != nil || !result.result.deleted || result.result.skipReason != "" {
		t.Fatalf("gc result deleted=%v skip_reason=%q err=%v", result.result.deleted, result.result.skipReason, result.err)
	}
	if err := <-writerDone; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("concurrent enqueue error = %v, want pgx.ErrNoRows from ownership fence", err)
	}
	var tasks int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE runtime_id = $1`, runtimeID).Scan(&tasks); err != nil {
		t.Fatalf("count raced tasks: %v", err)
	}
	if tasks != 0 {
		t.Fatalf("concurrent enqueue created %d tasks for deleted runtime", tasks)
	}
}

func TestRuntimeGC_TickBudgetBoundsWholeBatch(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	runtimeID := createRuntimeGCFixtureRuntime(t, ctx, "tick-budget")
	starter := &blockingRuntimeGCTxStarter{}

	startedAt := time.Now()
	gcRuntimesWithBudget(ctx, starter, db.New(testPool), nil, nil, 250*time.Millisecond)
	elapsed := time.Since(startedAt)

	if got := starter.begins.Load(); got != 1 {
		t.Fatalf("GC transactions started = %d, want 1 before the tick budget stopped the batch", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("GC exceeded its tick budget by too much: %s", elapsed)
	}
	var runtimeRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count budget-blocked runtime: %v", err)
	}
	if runtimeRows != 1 {
		t.Fatalf("runtime rows after tick timeout = %d, want 1", runtimeRows)
	}
}

type blockingRuntimeGCTxStarter struct {
	begins atomic.Int32
}

type runtimeGCPublishCall struct {
	kind                  string
	workspaceID           string
	actorType             string
	actorID               string
	action                string
	publishRuntimeRefresh bool
	cancelledTasks        int
}

type recordingRuntimeGCPublisher struct {
	calls []runtimeGCPublishCall
}

func (p *recordingRuntimeGCPublisher) PublishRuntimeTeardown(_ context.Context, result service.RuntimeTeardownResult, workspaceID, actorType, actorID, action string, publishRuntimeRefresh bool) {
	p.calls = append(p.calls, runtimeGCPublishCall{
		kind:                  "teardown",
		workspaceID:           workspaceID,
		actorType:             actorType,
		actorID:               actorID,
		action:                action,
		publishRuntimeRefresh: publishRuntimeRefresh,
		cancelledTasks:        len(result.CancelledTasks),
	})
}

func (p *recordingRuntimeGCPublisher) PublishRuntimeRefresh(workspaceID, actorType, actorID, action string) {
	p.calls = append(p.calls, runtimeGCPublishCall{
		kind:        "refresh",
		workspaceID: workspaceID,
		actorType:   actorType,
		actorID:     actorID,
		action:      action,
	})
}

func (s *blockingRuntimeGCTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.begins.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

type failRuntimeDeleteTxStarter struct {
	pool *pgxpool.Pool
}

func (s failRuntimeDeleteTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failRuntimeDeleteTx{Tx: tx}, nil
}

type failRuntimeDeleteTx struct {
	pgx.Tx
}

func (tx failRuntimeDeleteTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "-- name: DeleteAgentRuntime") {
		return pgconn.CommandTag{}, errors.New("injected runtime delete failure")
	}
	return tx.Tx.Exec(ctx, sql, arguments...)
}

type signalRuntimeLockTxStarter struct {
	pool    *pgxpool.Pool
	locked  chan struct{}
	proceed chan struct{}
}

func (s signalRuntimeLockTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return signalRuntimeLockTx{Tx: tx, locked: s.locked, proceed: s.proceed}, nil
}

type signalRuntimeLockTx struct {
	pgx.Tx
	locked  chan struct{}
	proceed chan struct{}
}

func (tx signalRuntimeLockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if !strings.Contains(sql, "-- name: LockAgentRuntime") {
		return row
	}
	return signalRuntimeLockRow{Row: row, locked: tx.locked, proceed: tx.proceed}
}

type signalRuntimeLockRow struct {
	pgx.Row
	locked  chan struct{}
	proceed chan struct{}
}

func (row signalRuntimeLockRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	select {
	case <-row.locked:
		// The shared teardown defensively re-acquires the same runtime lock.
		// Only the first acquisition is the synchronization point for this test.
	default:
		close(row.locked)
		<-row.proceed
	}
	return err
}

func waitForRuntimeGCBlockedWriter(ctx context.Context, pid int32) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := testPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE pid = $1 AND state = 'active' AND wait_event_type = 'Lock'
			)
		`, pid).Scan(&waiting); err != nil {
			return fmt.Errorf("poll writer lock: %w", err)
		}
		if waiting {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("writer backend %d never blocked on the runtime GC lock", pid)
}

func createRuntimeGCFixtureRuntime(t *testing.T, ctx context.Context, suffix string) string {
	return createRuntimeGCFixtureRuntimeForWorkspace(t, ctx, testWorkspaceID, suffix)
}

func createRuntimeGCFixtureRuntimeForWorkspace(t *testing.T, ctx context.Context, workspaceID, suffix string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, $2, 'cloud', 'runtime-gc-test', 'offline',
			'', '{}'::jsonb, $3, now() - interval '8 days')
		RETURNING id
	`, workspaceID, "Runtime GC "+suffix, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE runtime_id = $1`, runtimeID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createRuntimeGCFixtureAgent(t *testing.T, ctx context.Context, runtimeID, suffix string) string {
	return createRuntimeGCFixtureAgentForWorkspace(t, ctx, testWorkspaceID, runtimeID, suffix)
}

func createRuntimeGCFixtureAgentForWorkspace(t *testing.T, ctx context.Context, workspaceID, runtimeID, suffix string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "Runtime GC Agent "+suffix, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createRuntimeGCFixtureProfile(t *testing.T, ctx context.Context, suffix string) string {
	t.Helper()
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_profile (
			workspace_id, display_name, protocol_family, command_name, created_by
		)
		VALUES ($1, $2, 'codex', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Runtime GC Profile "+suffix, "runtime-gc-"+suffix, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("create runtime profile fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, profileID)
	})
	return profileID
}

func createRuntimeGCFixtureWorkspace(t *testing.T, ctx context.Context, suffix string) string {
	t.Helper()
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2 || '-' || substr(gen_random_uuid()::text, 1, 8), '')
		RETURNING id
	`, "Runtime GC "+suffix, "runtime-gc-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return workspaceID
}

func runtimeGCFixtureAgentID(t *testing.T, ctx context.Context) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent
		WHERE workspace_id = $1 AND name = 'Integration Test Agent'
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load integration fixture agent: %v", err)
	}
	return agentID
}

func createRuntimeGCFixtureTask(t *testing.T, ctx context.Context, runtimeID, agentID, status string, terminal bool) string {
	t.Helper()
	completedAt := "NULL"
	if terminal {
		completedAt = "now()"
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, context, completed_at)
		VALUES ($1, $2, $3, '{"runtime_gc_fixture":true}'::jsonb, `+completedAt+`)
		RETURNING id
	`, agentID, runtimeID, status).Scan(&taskID); err != nil {
		t.Fatalf("create %s task fixture: %v", status, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}
