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
	"github.com/multica-ai/multica/server/internal/events"
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
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatalf("unbind fixture agent: %v", err)
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

	workspaceID, deleted, blocked, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("gcRuntime: %v", err)
	}
	if !deleted || blocked {
		t.Fatalf("gc result deleted=%v blocked=%v, want deleted", deleted, blocked)
	}
	if workspaceID != testWorkspaceID {
		t.Fatalf("workspace id = %q, want %q", workspaceID, testWorkspaceID)
	}

	var runtimeRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count runtime: %v", err)
	}
	if runtimeRows != 0 {
		t.Fatalf("runtime rows = %d, want 0", runtimeRows)
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

			_, deleted, blocked, err := gcRuntime(ctx, testPool, db.New(testPool), parseUUID(runtimeID))
			if err != nil {
				t.Fatalf("gcRuntime: %v", err)
			}
			if deleted || !blocked {
				t.Fatalf("gc result deleted=%v blocked=%v, want safely blocked", deleted, blocked)
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
	_ = createRuntimeGCFixtureTask(t, ctx, blockedRuntimeID, agentID, "deferred", false)
	queries := db.New(testPool)

	candidates, err := queries.ListStaleOfflineRuntimeGCCandidates(ctx, db.ListStaleOfflineRuntimeGCCandidatesParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxPerTick:   runtimeGCBatchSize,
	})
	if err != nil {
		t.Fatalf("list GC candidates: %v", err)
	}
	foundDrainable := false
	for _, id := range candidates {
		idString := util.UUIDToString(id)
		if idString == blockedRuntimeID {
			t.Fatal("non-terminal task owner entered the bounded GC batch")
		}
		if idString == drainableRuntimeID {
			foundDrainable = true
		}
	}
	if !foundDrainable {
		t.Fatal("drainable runtime missing from GC candidates")
	}
	blocked, err := queries.CountStaleOfflineRuntimesBlockedByTasks(ctx, db.CountStaleOfflineRuntimesBlockedByTasksParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxRows:      runtimeGCBlockedScanLimit,
	})
	if err != nil {
		t.Fatalf("count blocked GC runtimes: %v", err)
	}
	if blocked < 1 {
		t.Fatalf("blocked runtime gauge source = %d, want at least 1", blocked)
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
	_, deleted, blocked, err := gcRuntime(ctx, starter, db.New(testPool), parseUUID(runtimeID))
	if err == nil || !strings.Contains(err.Error(), "injected runtime delete failure") {
		t.Fatalf("gcRuntime error = %v, want injected delete failure", err)
	}
	if deleted || blocked {
		t.Fatalf("gc result deleted=%v blocked=%v after error", deleted, blocked)
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
		deleted bool
		blocked bool
		err     error
	}
	gcDone := make(chan gcResult, 1)
	go func() {
		_, deleted, blocked, err := gcRuntime(ctx, starter, db.New(testPool), parseUUID(runtimeID))
		gcDone <- gcResult{deleted: deleted, blocked: blocked, err: err}
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
	if result.err != nil || !result.deleted || result.blocked {
		t.Fatalf("gc result deleted=%v blocked=%v err=%v", result.deleted, result.blocked, result.err)
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
	gcRuntimesWithBudget(ctx, starter, db.New(testPool), nil, events.New(), 250*time.Millisecond)
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
	close(row.locked)
	<-row.proceed
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
	`, testWorkspaceID, "Runtime GC "+suffix, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE runtime_id = $1`, runtimeID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createRuntimeGCFixtureAgent(t *testing.T, ctx context.Context, runtimeID, suffix string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, "Runtime GC Agent "+suffix, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
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
