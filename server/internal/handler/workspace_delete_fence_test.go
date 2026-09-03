package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// enqueueViaRealQuery creates a task through the generated CreateAgentTask query —
// the actual production enqueue path, fence included. Raw INSERTs in tests would
// bypass the fence, which lives in the application SQL rather than in a trigger.
func enqueueViaRealQuery(ctx context.Context, q *db.Queries, agentID, runtimeID, issueID string) error {
	params := db.CreateAgentTaskParams{
		AgentID:   parseUUID(agentID),
		RuntimeID: parseUUID(runtimeID),
		Priority:  0,
	}
	if issueID != "" {
		params.IssueID = parseUUID(issueID)
	}
	_, err := q.CreateAgentTask(ctx, params)
	return err
}

// waitForBlockedWriter blocks until the given backend is waiting on a lock, so a
// race test can be sure the writer really is queued before the other side commits.
// The pid comes from the writer itself rather than being guessed from statement
// text, which on a shared test database could match an unrelated backend. Fails the
// test on timeout rather than proceeding, so a missed race shows up as a failure
// instead of a vacuous pass.
func waitForBlockedWriter(t *testing.T, ctx context.Context, pid int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := testPool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE pid = $1 AND state = 'active' AND wait_event_type = 'Lock'
)
`, pid).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("backend %d never parked on a lock; the race was not set up", pid)
}

// TestTaskWriteFence_BlocksOnWorkspaceLockAlone shows the fence, and only the
// fence, standing between a concurrent enqueue and a workspace being torn down.
//
// The holder locks ONLY the workspace row — no agent, issue or runtime row — so no
// foreign key on agent_task_queue has anything to block on: none of them
// references workspace. Anything that blocks here is the fence taking FOR KEY
// SHARE on the owners' workspace row from inside the writer's own statement.
func TestTaskWriteFence_BlocksOnWorkspaceLockAlone(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "fenceonly")

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer holder.Rollback(ctx)
	// Exactly what LockWorkspaceForDelete does, and nothing else.
	if _, err := holder.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, f.victimID); err != nil {
		t.Fatalf("lock workspace row: %v", err)
	}

	blockedEnqueue := func(name, agentID, runtimeID, issueID string) {
		t.Helper()
		writer, err := testPool.Begin(ctx)
		if err != nil {
			t.Fatalf("%s: begin: %v", name, err)
		}
		defer writer.Rollback(ctx)
		if _, err := writer.Exec(ctx, "SET LOCAL lock_timeout = 750"); err != nil {
			t.Fatalf("%s: set lock_timeout: %v", name, err)
		}
		err = enqueueViaRealQuery(ctx, testHandler.Queries.WithTx(writer), agentID, runtimeID, issueID)
		if err == nil {
			t.Errorf("%s: enqueue committed while the workspace row was locked; the fence is not in the write path", name)
			return
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Errorf("%s: got %v, want lock_not_available (55P03)", name, err)
		}
	}

	blockedEnqueue("enqueue via the victim's agent", f.victimAgent, f.neighbourRuntime, f.neighbourIssue)
	blockedEnqueue("enqueue via the victim's runtime", f.neighbourAgent, f.victimRuntime, f.neighbourIssue)
	blockedEnqueue("enqueue via the victim's issue", f.neighbourAgent, f.neighbourRuntime, f.victimIssue)

	// Reassignment onto the victim's runtime is fenced too.
	reassign, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reassign: %v", err)
	}
	defer reassign.Rollback(ctx)
	if _, err := reassign.Exec(ctx, "SET LOCAL lock_timeout = 750"); err != nil {
		t.Fatalf("reassign: set lock_timeout: %v", err)
	}
	_, err = testHandler.Queries.WithTx(reassign).ReassignTasksToRuntime(ctx, db.ReassignTasksToRuntimeParams{
		NewRuntimeID: parseUUID(f.victimRuntime),
		OldRuntimeID: parseUUID(f.neighbourRuntime),
	})
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Errorf("reassignment onto the victim's runtime: got %v, want lock_not_available (55P03)", err)
	}

	// A workspace nobody is deleting is unaffected.
	unrelated, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unrelated: %v", err)
	}
	defer unrelated.Rollback(ctx)
	if _, err := unrelated.Exec(ctx, "SET LOCAL lock_timeout = 750"); err != nil {
		t.Fatalf("unrelated: set lock_timeout: %v", err)
	}
	if err := enqueueViaRealQuery(ctx, testHandler.Queries.WithTx(unrelated),
		f.neighbourAgent, f.neighbourRuntime, f.neighbourIssue); err != nil {
		t.Errorf("neighbour-only enqueue was blocked by the victim's workspace lock: %v", err)
	}

	// Status-only updates are the hot path and must not touch the fence at all.
	statusUpdate, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin status update: %v", err)
	}
	defer statusUpdate.Rollback(ctx)
	if _, err := statusUpdate.Exec(ctx, "SET LOCAL lock_timeout = 750"); err != nil {
		t.Fatalf("status update: set lock_timeout: %v", err)
	}
	if _, err := statusUpdate.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'failed' WHERE id = $1`, f.taskViaAgent); err != nil {
		t.Errorf("status-only update was blocked: %v", err)
	}
}

// TestTaskWriteFence_QueuedWriterCannotLandAfterTeardownCommits is the race the
// review asked for, run as an actual race: a writer queues behind the workspace
// lock while teardown is in flight, teardown commits, and the writer must then be
// unable to put a row in the table.
//
// It also discriminates the fence from the foreign keys. The writer's failure is
// "no row written" — CreateAgentTask is a :one, so it comes back as
// pgx.ErrNoRows because the fence's predicate was false. An FK doing the work
// instead would surface as SQLSTATE 23503, which the assertion rejects.
func TestTaskWriteFence_QueuedWriterCannotLandAfterTeardownCommits(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "queuedwriter")

	teardown, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = teardown.Rollback(ctx)
		}
	}()

	// The teardown transaction, in the order the handler does it.
	if _, err := teardown.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, f.victimID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	var wg sync.WaitGroup
	var writerErr error
	writerPID := make(chan int32, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		// A dedicated connection so the writer can report the backend the race
		// waits on, and so the enqueue still runs as its own autocommit statement
		// the way a real one does.
		conn, err := testPool.Acquire(bg)
		if err != nil {
			writerErr = err
			close(writerPID)
			return
		}
		defer conn.Release()

		var pid int32
		if err := conn.QueryRow(bg, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			writerErr = err
			close(writerPID)
			return
		}
		writerPID <- pid

		// No lock_timeout: this writer is supposed to wait for teardown, which is
		// what a real enqueue arriving mid-teardown does.
		writerErr = enqueueViaRealQuery(bg, db.New(conn), f.victimAgent, f.victimRuntime, f.victimIssue)
	}()

	pid, ok := <-writerPID
	if !ok {
		wg.Wait()
		t.Fatalf("writer could not start: %v", writerErr)
	}

	// Wait until the writer is genuinely parked on a lock rather than trusting a
	// fixed sleep: on a loaded machine a sleep can elapse before the writer even
	// reaches the fence, and the test would then pass without exercising the race.
	waitForBlockedWriter(t, ctx, pid)

	if _, err := teardown.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, f.victimID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	if err := teardown.Commit(ctx); err != nil {
		t.Fatalf("commit teardown: %v", err)
	}
	committed = true

	wg.Wait()

	if writerErr == nil {
		t.Fatal("the queued enqueue landed a task after teardown committed; the fence did not hold")
	}
	if !errors.Is(writerErr, pgx.ErrNoRows) {
		var pgErr *pgconn.PgError
		if errors.As(writerErr, &pgErr) && pgErr.Code == "23503" {
			t.Fatalf("the write was rejected by a foreign key (%s), not by the fence; "+
				"the fence must hold on its own once the legacy FKs go away", pgErr.Code)
		}
		t.Fatalf("writer error = %v, want pgx.ErrNoRows from the fence returning false", writerErr)
	}

	var leftover int
	if err := testPool.QueryRow(ctx, `
SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 OR runtime_id = $2 OR issue_id = $3
`, f.victimAgent, f.victimRuntime, f.victimIssue).Scan(&leftover); err != nil {
		t.Fatalf("count leftovers: %v", err)
	}
	if leftover != 0 {
		t.Errorf("%d task rows are left pointing at the deleted workspace's owners", leftover)
	}
}

// TestTaskWriteFence_LocksWorkspacesInIDOrder covers the deadlock surface the
// review flagged: a task may reference owners in different workspaces, so the
// fence can lock more than one workspace row, and two writers touching the same
// pair from opposite directions must not be able to take them in opposite orders.
//
// The fence sorts by workspace id, so both writers queue on the lower id first.
// The test runs the two mirrored writes concurrently and requires that both
// succeed — a deadlock would surface as 40P01 on one of them.
func TestTaskWriteFence_LocksWorkspacesInIDOrder(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "lockorder")

	const rounds = 25

	// One pending task per (issue, agent) is enforced by a partial unique index, so
	// each round needs its own issue pair; otherwise the rounds collide on that
	// index instead of exercising the lock order.
	victimIssues := make([]string, rounds)
	neighbourIssues := make([]string, rounds)
	for i := 0; i < rounds; i++ {
		if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
VALUES ($1, $2, 'member', $3, $4) RETURNING id
`, f.victimID, "lock-order victim "+strconv.Itoa(i), testUserID, i+100).Scan(&victimIssues[i]); err != nil {
			t.Fatalf("seed victim issue %d: %v", i, err)
		}
		if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
VALUES ($1, $2, 'member', $3, $4) RETURNING id
`, f.neighbourID, "lock-order neighbour "+strconv.Itoa(i), testUserID, i+100).Scan(&neighbourIssues[i]); err != nil {
			t.Fatalf("seed neighbour issue %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2*rounds)
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		// Owners drawn from both workspaces, in mirrored roles.
		go func(i int) {
			defer wg.Done()
			errs[2*i] = enqueueViaRealQuery(ctx, testHandler.Queries,
				f.victimAgent, f.neighbourRuntime, neighbourIssues[i])
		}(i)
		go func(i int) {
			defer wg.Done()
			errs[2*i+1] = enqueueViaRealQuery(ctx, testHandler.Queries,
				f.neighbourAgent, f.victimRuntime, victimIssues[i])
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("write %d deadlocked (40P01): the fence is not locking workspaces in a fixed order", i)
		}
		t.Errorf("write %d failed: %v", i, err)
	}

	if _, err := testPool.Exec(ctx, `
DELETE FROM agent_task_queue
WHERE (agent_id = $1 AND runtime_id = $2) OR (agent_id = $3 AND runtime_id = $4)
`, f.victimAgent, f.neighbourRuntime, f.neighbourAgent, f.victimRuntime); err != nil {
		t.Fatalf("clean up: %v", err)
	}
}

// TestMergeLegacyRuntime_KeepsOldRuntimeWhenFenceRefuses is the regression test
// for the failure mode the review found: the fence refusing looked exactly like
// "the old runtime had no tasks", so the merge went on to delete the old runtime
// and agent_task_queue's ON DELETE CASCADE quietly took its history with it.
//
// Here the merge target has vanished before the reassignment runs, so the fence
// returns false. The old runtime and its task must both survive, and nothing may
// be recorded on the target.
func TestMergeLegacyRuntime_KeepsOldRuntimeWhenFenceRefuses(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "mergefence")

	// A target runtime id that does not resolve — the same state a merge sees when
	// the target's workspace was torn down a moment ago.
	vanishedTarget := parseUUID("00000000-0000-0000-0000-0000000000fe")

	err := testHandler.mergeLegacyRuntime(ctx, vanishedTarget, parseUUID(f.victimRuntime), "legacy-daemon", "delete-test")
	if !errors.Is(err, errRuntimeMergeFenced) {
		t.Fatalf("mergeLegacyRuntime = %v, want errRuntimeMergeFenced", err)
	}

	if !rowExists(t, "agent_runtime", f.victimRuntime) {
		t.Error("the old runtime was deleted even though the reassignment was fenced")
	}
	if !rowExists(t, "agent_task_queue", f.taskViaRuntime) {
		t.Error("the old runtime's task was deleted by the cascade behind a fenced merge")
	}
	var stillOnOldRuntime bool
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id = $1 FROM agent_task_queue WHERE id = $2`,
		f.victimRuntime, f.taskViaRuntime).Scan(&stillOnOldRuntime); err != nil {
		t.Fatalf("read task runtime: %v", err)
	}
	if !stillOnOldRuntime {
		t.Error("the task was re-pointed at the vanished runtime")
	}

	// And the happy path still merges, so the abort is not unconditional.
	var freshRuntime string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
VALUES ($1, 'merge target', 'cloud', 'delete-test', 'offline', '', '{}'::jsonb, $2)
RETURNING id
`, f.victimID, testUserID).Scan(&freshRuntime); err != nil {
		t.Fatalf("create merge target: %v", err)
	}
	if err := testHandler.mergeLegacyRuntime(ctx, parseUUID(freshRuntime), parseUUID(f.victimRuntime),
		"legacy-daemon", "delete-test"); err != nil {
		t.Fatalf("merge onto a live runtime: %v", err)
	}
	if rowExists(t, "agent_runtime", f.victimRuntime) {
		t.Error("the old runtime survived a merge that should have completed")
	}
	if !rowExists(t, "agent_task_queue", f.taskViaRuntime) {
		t.Error("a completed merge lost the old runtime's task")
	}
}

// TestMergeLegacyRuntime_RollsBackWhenAStepFails is the fault-injection half of the
// merge contract: the four statements have to land together or not at all.
//
// Run as autocommit statements the merge could commit the task reassignment and
// then fail on the agent reassignment, leaving tasks on the new runtime and their
// agents on the old one while daemon registration still reported success. Worse,
// the fence's workspace lock ended with its own statement, so a teardown could slip
// into that gap, delete the target runtime and clear runtime_id on the tasks that
// had just moved — history the next merge could no longer find, because it looks up
// tasks by the OLD runtime id.
//
// The failure is injected in the database rather than through a code seam, so the
// test exercises the real statements: a trigger makes any agent row moving onto the
// merge target raise.
func TestMergeLegacyRuntime_RollsBackWhenAStepFails(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "mergerollback")

	var target string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
VALUES ($1, 'rollback merge target', 'cloud', 'delete-test', 'offline', '', '{}'::jsonb, $2)
RETURNING id
`, f.victimID, testUserID).Scan(&target); err != nil {
		t.Fatalf("create merge target: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
CREATE OR REPLACE FUNCTION mul5999_fail_agent_move() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    IF NEW.name = 'victim agent' AND NEW.runtime_id IS DISTINCT FROM OLD.runtime_id THEN
        RAISE EXCEPTION 'injected failure moving agent onto the merge target';
    END IF;
    RETURN NEW;
END
$fn$;
`); err != nil {
		t.Fatalf("create injection function: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
CREATE TRIGGER mul5999_fail_agent_move
BEFORE UPDATE OF runtime_id ON agent
FOR EACH ROW EXECUTE FUNCTION mul5999_fail_agent_move()
`); err != nil {
		t.Fatalf("create injection trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DROP TRIGGER IF EXISTS mul5999_fail_agent_move ON agent`)
		_, _ = testPool.Exec(bg, `DROP FUNCTION IF EXISTS mul5999_fail_agent_move()`)
	})

	err := testHandler.mergeLegacyRuntime(ctx, parseUUID(target), parseUUID(f.victimRuntime),
		"legacy-daemon", "delete-test")
	if err == nil {
		t.Fatal("merge reported success even though the agent reassignment failed")
	}
	if !strings.Contains(err.Error(), "reassign agents") {
		t.Errorf("error = %v, want the agent reassignment failure", err)
	}

	// Everything must look exactly as it did before the call.
	var taskRuntime, agentRuntime string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent_task_queue WHERE id = $1`, f.taskViaRuntime).Scan(&taskRuntime); err != nil {
		t.Fatalf("read task runtime: %v", err)
	}
	if taskRuntime != f.victimRuntime {
		t.Errorf("task moved to %s; the task reassignment was not rolled back", taskRuntime)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE id = $1`, f.victimAgent).Scan(&agentRuntime); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	if agentRuntime != f.victimRuntime {
		t.Errorf("agent runtime = %s, want the original %s", agentRuntime, f.victimRuntime)
	}
	if !rowExists(t, "agent_runtime", f.victimRuntime) {
		t.Error("the old runtime was deleted by a merge that failed")
	}
	if !rowExists(t, "agent_runtime", target) {
		t.Error("the merge target disappeared")
	}
	var legacyRecorded bool
	if err := testPool.QueryRow(ctx,
		`SELECT legacy_daemon_id IS NOT NULL FROM agent_runtime WHERE id = $1`, target).Scan(&legacyRecorded); err != nil {
		t.Fatalf("read legacy daemon id: %v", err)
	}
	if legacyRecorded {
		t.Error("the legacy daemon_id audit write survived a rolled-back merge")
	}

	// With the injection removed the same merge completes, so the rollback above
	// was not just a permanently broken fixture.
	if _, err := testPool.Exec(ctx, `DROP TRIGGER mul5999_fail_agent_move ON agent`); err != nil {
		t.Fatalf("drop injection trigger: %v", err)
	}
	if err := testHandler.mergeLegacyRuntime(ctx, parseUUID(target), parseUUID(f.victimRuntime),
		"legacy-daemon", "delete-test"); err != nil {
		t.Fatalf("merge after removing the injection: %v", err)
	}
	if rowExists(t, "agent_runtime", f.victimRuntime) {
		t.Error("the old runtime survived a completed merge")
	}
}

// TestRuntimeMerge_LateWriteToOldRuntimeIsFencedNotCascaded is the regression test
// for the race the review reproduced: an enqueue that arrives against the OLD
// runtime after the merge has scanned its tasks.
//
// Before this fix the writer was not blocked at all — writers hold FOR KEY SHARE on
// the workspace and so did the merge, and two KEY SHARE locks do not conflict — so
// the insert committed and was then silently removed by agent_task_queue's
// ON DELETE CASCADE when the merge deleted the old runtime. The reviewer measured
// exactly that: `INSERT 0 1`, then the row count going from 1 to 0.
//
// The merge's statement sequence is driven by hand here so it can be stopped after
// the agent update and before the old runtime is deleted, which is where the window
// used to be. The writer must block on the fence, and once the merge commits it must
// come back as "no row written" — not 23503, and not a row that exists briefly and
// then disappears.
func TestRuntimeMerge_LateWriteToOldRuntimeIsFencedNotCascaded(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "latewrite")

	var target string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
VALUES ($1, 'late write merge target', 'cloud', 'delete-test', 'offline', '', '{}'::jsonb, $2)
RETURNING id
`, f.victimID, testUserID).Scan(&target); err != nil {
		t.Fatalf("create merge target: %v", err)
	}

	merge, err := testHandler.TxStarter.Begin(ctx)
	if err != nil {
		t.Fatalf("begin merge tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = merge.Rollback(ctx)
		}
	}()
	qtx := testHandler.Queries.WithTx(merge)
	runtimeIDs := []pgtype.UUID{parseUUID(f.victimRuntime), parseUUID(target)}

	// The merge, up to the point just before the old runtime is deleted.
	if err := qtx.LockWorkspaceForRuntimeMerge(ctx, runtimeIDs); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}
	locked, err := qtx.LockRuntimesForMerge(ctx, runtimeIDs)
	if err != nil {
		t.Fatalf("lock runtimes: %v", err)
	}
	if len(locked) != 2 {
		t.Fatalf("locked %d runtimes, want 2", len(locked))
	}
	reassignment, err := qtx.ReassignTasksToRuntime(ctx, db.ReassignTasksToRuntimeParams{
		NewRuntimeID: parseUUID(target),
		OldRuntimeID: parseUUID(f.victimRuntime),
	})
	if err != nil {
		t.Fatalf("reassign tasks: %v", err)
	}
	if !reassignment.FenceOk {
		t.Fatal("the merge's own fence refused a live target")
	}
	if _, err := qtx.ReassignAgentsToRuntime(ctx, db.ReassignAgentsToRuntimeParams{
		NewRuntimeID: parseUUID(target),
		OldRuntimeID: parseUUID(f.victimRuntime),
	}); err != nil {
		t.Fatalf("reassign agents: %v", err)
	}

	// A concurrent enqueue that still believes in the old runtime.
	var wg sync.WaitGroup
	var writerErr error
	writerPID := make(chan int32, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		conn, err := testPool.Acquire(bg)
		if err != nil {
			writerErr = err
			close(writerPID)
			return
		}
		defer conn.Release()
		var pid int32
		if err := conn.QueryRow(bg, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			writerErr = err
			close(writerPID)
			return
		}
		writerPID <- pid
		writerErr = enqueueViaRealQuery(bg, db.New(conn), f.victimAgent, f.victimRuntime, f.victimIssue)
	}()

	pid, ok := <-writerPID
	if !ok {
		wg.Wait()
		t.Fatalf("writer could not start: %v", writerErr)
	}
	// The whole point: the writer must be parked, not sailing past the merge.
	waitForBlockedWriter(t, ctx, pid)

	if err := qtx.DeleteAgentRuntime(ctx, parseUUID(f.victimRuntime)); err != nil {
		t.Fatalf("delete old runtime: %v", err)
	}
	if err := merge.Commit(ctx); err != nil {
		t.Fatalf("commit merge: %v", err)
	}
	committed = true

	wg.Wait()

	if writerErr == nil {
		t.Fatal("the late enqueue landed against a runtime the merge was deleting; " +
			"it would have been removed by ON DELETE CASCADE")
	}
	var pgErr *pgconn.PgError
	if errors.As(writerErr, &pgErr) && pgErr.Code == "23503" {
		t.Fatalf("the late write was rejected by a foreign key (%s), not by the fence", pgErr.Code)
	}
	if !errors.Is(writerErr, pgx.ErrNoRows) {
		t.Fatalf("writer error = %v, want pgx.ErrNoRows from the fence returning false", writerErr)
	}

	// The merge's own history survived, and nothing is pointing at the deleted runtime.
	if !rowExists(t, "agent_task_queue", f.taskViaRuntime) {
		t.Error("the merge lost the old runtime's task")
	}
	var onTarget bool
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id = $1 FROM agent_task_queue WHERE id = $2`, target, f.taskViaRuntime).Scan(&onTarget); err != nil {
		t.Fatalf("read task runtime: %v", err)
	}
	if !onTarget {
		t.Error("the task was not moved onto the merge target")
	}
}

// TestLockTaskOwnerRows_RejectsUnresolvableOwner pins the hole the review
// found in the first version of this fence: counting only the owners that still
// resolve made a deleted owner invisible, and the statement fell through to the
// foreign key. A reference that does not resolve must make the predicate false on
// its own.
func TestLockTaskOwnerRows_RejectsUnresolvableOwner(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "unresolvable")
	missing := "00000000-0000-0000-0000-0000000000ff"

	for name, args := range map[string][3]any{
		"missing agent":               {missing, nil, nil},
		"missing issue":               {nil, missing, nil},
		"missing runtime":             {nil, nil, missing},
		"one of three owners missing": {f.victimAgent, missing, f.victimRuntime},
	} {
		var ok bool
		if err := testPool.QueryRow(ctx,
			`SELECT lock_task_owner_rows($1, $2, $3)`, args[0], args[1], args[2]).Scan(&ok); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ok {
			t.Errorf("%s: fence returned true; the write would fall through to the foreign key", name)
		}
	}

	var ok bool
	if err := testPool.QueryRow(ctx, `SELECT lock_task_owner_rows($1, $2, $3)`,
		f.victimAgent, f.victimIssue, f.victimRuntime).Scan(&ok); err != nil {
		t.Fatalf("all owners present: %v", err)
	}
	if !ok {
		t.Error("fence returned false for a fully resolvable row")
	}
}

var _ = pgtype.UUID{}
