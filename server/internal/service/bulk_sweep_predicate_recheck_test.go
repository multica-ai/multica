package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The bulk sweep's candidate peek takes NO lock (that is what lets it lock the
// workspace first), so between the peek and the per-workspace transaction a
// candidate can legitimately stop being eligible. Re-locking on a broad "status is
// one of the active values" predicate would then kill a task that just became
// healthy — the exact window MUL-4332's review reproduced on the queued-TTL sweeper:
// the peek reads an expired `queued` task, a daemon commits queued -> dispatched,
// and the sweep flips that just-started task to `failed` with a `queued_expired`
// event.
//
// This drives the REAL production pair — PeekExpiredQueuedTasks for the candidate
// read and LockExpiredQueuedTasksByIDsForFail for the re-lock — and lands the REAL
// daemon claim (ClaimAgentTask) in between. With the predicate-specific re-lock the
// dispatched task drops out untouched; with the old shared status set it is failed.
func TestExpiredQueuedSweepSkipsTaskDispatchedAfterThePeek(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	// One task that is genuinely TTL-expired in `queued` — a real sweep candidate.
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, now() - interval '10 minutes')
		RETURNING id`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	claimed := false
	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			// The real, unlocked candidate read. Scoped to this test's task so a
			// neighbouring test's backlog cannot join the batch.
			rows, e := q.PeekExpiredQueuedTasks(ctx, db.PeekExpiredQueuedTasksParams{
				TtlSecs:    60,
				MaxPerTick: 100,
			})
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 1)
			for _, r := range rows {
				if util.UUIDToString(r.ID) == taskID {
					mine = append(mine, r)
				}
			}
			if len(mine) != 1 {
				t.Errorf("peek returned %d rows for the seeded task, want 1", len(mine))
			}

			// THE RACE: a daemon claims the task for real, committing
			// queued -> dispatched, after the peek has already banked it as a
			// candidate and before the sweep re-locks it.
			c, cErr := queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
				AgentID:          util.MustParseUUID(agentID),
				PrepareLeaseSecs: 300,
			})
			if cErr != nil {
				t.Errorf("daemon claim: %v", cErr)
			} else if util.UUIDToString(c.ID) != taskID {
				t.Errorf("daemon claimed %s, want the seeded task %s", util.UUIDToString(c.ID), taskID)
			} else {
				claimed = true
			}
			return mine, nil
		},
		// The production re-lock: the same queued-TTL predicate, now under the task
		// row lock.
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.LockExpiredQueuedTasksByIDsForFail(ctx, db.LockExpiredQueuedTasksByIDsForFailParams{
				Ids:     ids,
				TtlSecs: 60,
			})
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "task expired in queue", Valid: true},
				FailureReason: pgtype.Text{String: "queued_expired", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("bulk sweep: %v", err)
	}
	if !claimed {
		t.Fatal("the daemon claim did not land — the race was not reproduced")
	}

	if len(failed) != 0 {
		t.Errorf("sweep failed %d task(s), want 0 — a task dispatched after the peek is no longer queued-expired and must not be reaped", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "dispatched" {
		t.Errorf("task status = %q, want dispatched (the sweep killed a task a daemon had just started)", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 0 {
		t.Errorf("task events = %d, want 0 — no queued_expired event may be emitted for a dispatched task", n)
	}
}

// A bulk sweep tick spans many workspaces and runs ONE transaction per workspace,
// so it can succeed partially: workspace A's batch errors while workspace B's fact
// + event are already committed. Those committed rows are terminal and will never be
// re-selected by a later tick, so their post-commit side effects — auto-retry, stuck
// issue reset, agent reconcile, realtime — are the sweep's only chance to run them.
//
// FailBulkTasksWithEvents therefore returns BOTH the committed rows and the
// aggregate error, and every caller must dispatch the rows BEFORE acting on the
// error. This pins that contract end to end: workspace A's transaction fails for
// real, workspace B commits, and running the production dispatch step on the
// returned rows produces B's real, DB-observable side effect (its stuck issue is
// reset to todo with an issue.status_changed event). A caller that returned early
// on the error would lose that reset permanently.
func TestBulkSweepDispatchesCommittedWorkspaceWhenAnotherWorkspaceFails(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	// Two INDEPENDENT workspaces, each with its own agent, runtime and issue.
	_, _, agentA, issueA := seedAttributionFixture(t, pool)
	_, _, agentB, issueB := seedAttributionFixture(t, pool)

	// Both issues are stuck in_progress, so the failed task's post-commit pipeline
	// has a real reset to perform.
	for _, id := range []string{issueA, issueB} {
		if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, id); err != nil {
			t.Fatalf("set issue in_progress: %v", err)
		}
	}

	// One TTL-expired queued task per workspace. A is older so it is swept first —
	// the error lands before B's workspace is even reached.
	seed := func(agentID, issueID, age string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, now() - $3::interval)
			RETURNING id`, agentID, issueID, age).Scan(&id); err != nil {
			t.Fatalf("seed queued task: %v", err)
		}
		return id
	}
	taskA := seed(agentA, issueA, "20 minutes")
	taskB := seed(agentB, issueB, "10 minutes")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, []string{taskA, taskB})
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = ANY($1::uuid[])`,
			[]string{taskA, taskB, issueA, issueB})
	})

	// Workspace A's transaction blows up (a transient DB failure inside that one
	// workspace's batch); workspace B's runs the real fail.
	boom := errors.New("transient DB blip in workspace A")
	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			rows, e := q.PeekExpiredQueuedTasks(ctx, db.PeekExpiredQueuedTasksParams{
				TtlSecs:    60,
				MaxPerTick: 100,
			})
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 2)
			for _, r := range rows {
				switch util.UUIDToString(r.ID) {
				case taskA, taskB:
					mine = append(mine, r)
				}
			}
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.LockExpiredQueuedTasksByIDsForFail(ctx, db.LockExpiredQueuedTasksByIDsForFailParams{
				Ids:     ids,
				TtlSecs: 60,
			})
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			for _, id := range ids {
				if util.UUIDToString(id) == taskA {
					return nil, boom
				}
			}
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "task expired in queue", Valid: true},
				FailureReason: pgtype.Text{String: "queued_expired", Valid: true},
			})
		})

	// Both return values are meaningful AT THE SAME TIME.
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the workspace A failure to surface", err)
	}
	if len(failed) != 1 || util.UUIDToString(failed[0].ID) != taskB {
		t.Fatalf("returned rows = %v, want exactly workspace B's task %s — a failing workspace must not swallow a committed one",
			failed, taskB)
	}

	// Workspace A rolled back entirely; workspace B committed fact + event.
	if s := taskStatusForTest(t, pool, taskA); s != "queued" {
		t.Errorf("workspace A task = %q, want queued (its transaction rolled back)", s)
	}
	if n := subjectEventCount(t, pool, taskA); n != 0 {
		t.Errorf("workspace A events = %d, want 0", n)
	}
	if s := taskStatusForTest(t, pool, taskB); s != "failed" {
		t.Errorf("workspace B task = %q, want failed", s)
	}
	if n := subjectEventCount(t, pool, taskB); n != 1 {
		t.Errorf("workspace B events = %d, want 1", n)
	}

	// The caller's contract: dispatch the committed rows BEFORE surfacing the error.
	// This is the exact production ordering the sweepers and recoverOrphansPage use.
	svc.CaptureQueuedExpiredTasks(ctx, failed)
	svc.HandleFailedTasks(ctx, failed)

	// B's post-commit side effect actually landed — the reset an early return would
	// have dropped forever, since taskB is already terminal and no tick re-selects it.
	if s := issueStatusForTest(t, pool, issueB); s != "todo" {
		t.Errorf("workspace B issue = %q, want todo — the committed row's post-commit reset was lost", s)
	}
	if n := subjectEventCount(t, pool, issueB); n != 1 {
		t.Errorf("workspace B issue events = %d, want 1 issue.status_changed", n)
	}
	// A's task never failed, so nothing about A may have moved.
	if s := issueStatusForTest(t, pool, issueA); s != "in_progress" {
		t.Errorf("workspace A issue = %q, want in_progress (its sweep rolled back)", s)
	}
}

// The same peek-then-lock window on the offline-runtime sweeper: a daemon that
// re-registers between the peek and the transaction flips its runtime back to
// `online`, which makes its in-flight tasks ineligible again. Killing them would
// fail work a live daemon is actively running.
func TestOfflineRuntimeSweepSkipsTaskWhoseRuntimeCameBackOnline(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	runtimeID := runtimeIDForAgentTest(t, pool, agentID)

	// The runtime is offline with a task still in flight — a real sweep candidate.
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("take runtime offline: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			rows, e := q.PeekTasksForOfflineRuntimes(ctx, 100)
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 1)
			for _, r := range rows {
				if util.UUIDToString(r.ID) == taskID {
					mine = append(mine, r)
				}
			}
			if len(mine) != 1 {
				t.Errorf("peek returned %d rows for the seeded task, want 1", len(mine))
			}
			// THE RACE: the daemon re-registers and its runtime is online again.
			if _, uErr := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID); uErr != nil {
				t.Errorf("bring runtime back online: %v", uErr)
			}
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.LockOfflineRuntimeTasksByIDsForFail(ctx, ids)
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "runtime went offline", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("bulk sweep: %v", err)
	}
	if len(failed) != 0 {
		t.Errorf("sweep failed %d task(s), want 0 — the runtime is online again", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "running" {
		t.Errorf("task status = %q, want running (the sweep killed a task on a live runtime)", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 0 {
		t.Errorf("task events = %d, want 0", n)
	}
}

// The same peek-then-lock window on the stale sweeper: the daemon refreshes the
// task's prepare lease (ExtendAgentTaskPrepareLease, renewed every ~15s between
// claim and StartTask) between the peek and the transaction, which proves it is
// alive and makes the task ineligible again.
func TestStaleSweepSkipsTaskWhosePrepareLeaseWasRefreshed(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	runtimeID := runtimeIDForAgentTest(t, pool, agentID)

	// Dispatched long ago with an already-expired prepare lease — a real candidate.
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, prepare_lease_expires_at)
		VALUES ($1, $2, $3, 'dispatched', 0, now() - interval '30 minutes', now() - interval '5 minutes')
		RETURNING id`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed dispatched task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			rows, e := q.PeekStaleTasksToFail(ctx, db.PeekStaleTasksToFailParams{
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
				MaxPerTick:            100,
			})
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 1)
			for _, r := range rows {
				if util.UUIDToString(r.ID) == taskID {
					mine = append(mine, r)
				}
			}
			if len(mine) != 1 {
				t.Errorf("peek returned %d rows for the seeded task, want 1", len(mine))
			}
			// THE RACE: the daemon proves it is alive by extending the prepare lease.
			if _, eErr := queries.ExtendAgentTaskPrepareLease(ctx, db.ExtendAgentTaskPrepareLeaseParams{
				ID:        util.MustParseUUID(taskID),
				RuntimeID: runtimeID,
				LeaseSecs: 300,
			}); eErr != nil {
				t.Errorf("extend prepare lease: %v", eErr)
			}
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.LockStaleTasksByIDsForFail(ctx, db.LockStaleTasksByIDsForFailParams{
				Ids:                   ids,
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
			})
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "task timed out", Valid: true},
				FailureReason: pgtype.Text{String: "timeout", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("bulk sweep: %v", err)
	}
	if len(failed) != 0 {
		t.Errorf("sweep failed %d task(s), want 0 — the prepare lease is live again", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "dispatched" {
		t.Errorf("task status = %q, want dispatched (the sweep killed a task whose daemon is alive)", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 0 {
		t.Errorf("task events = %d, want 0", n)
	}
}

// The task row lock covers the TASK row only. Both runtime-dependent sweepers also
// test agent_runtime — status for the offline sweep, last_seen_at for the running
// branch of the stale sweep — and that row stays writable while we hold the task
// lock. So the eligibility re-check has to live in the statement that actually
// changes the task status, with the runtime row held, or a daemon that recovers in
// the lock -> fail window still loses the task it is actively running.
//
// The two tests below commit the REAL recovery (MarkAgentRuntimeOnline /
// TouchAgentRuntimeLastSeen) AFTER the production re-lock query has returned and the
// task rows are already locked — the window the earlier peek-time regressions do not
// reach. FailOfflineRuntimeTasksByIDs / FailStaleTasksByIDs re-apply the runtime
// condition and take the runtime row FOR SHARE, so the recovered task drops out.

func TestOfflineRuntimeSweepSkipsRuntimeThatCameOnlineAfterTheTaskLock(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	runtimeID := runtimeIDForAgentTest(t, pool, agentID)

	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("take runtime offline: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	recovered := false
	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			rows, e := q.PeekTasksForOfflineRuntimes(ctx, 100)
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 1)
			for _, r := range rows {
				if util.UUIDToString(r.ID) == taskID {
					mine = append(mine, r)
				}
			}
			if len(mine) != 1 {
				t.Errorf("peek returned %d rows for the seeded task, want 1", len(mine))
			}
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			locked, lErr := qtx.LockOfflineRuntimeTasksByIDsForFail(ctx, ids)
			if lErr != nil {
				return nil, lErr
			}
			if len(locked) != 1 {
				t.Errorf("re-lock returned %d rows, want 1 — the race needs a locked candidate", len(locked))
			}
			// THE RACE: the task row is now locked and the sweep has already judged it
			// eligible, but the runtime row is untouched — the daemon re-registers on
			// another connection and that commit lands before the fail statement.
			if _, mErr := queries.MarkAgentRuntimeOnline(ctx, runtimeID); mErr != nil {
				t.Errorf("mark runtime online: %v", mErr)
			} else {
				recovered = true
			}
			return locked, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailOfflineRuntimeTasksByIDs(ctx, db.FailOfflineRuntimeTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "runtime went offline", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("bulk sweep: %v", err)
	}
	if !recovered {
		t.Fatal("the runtime never came back online — the race was not reproduced")
	}

	if len(failed) != 0 {
		t.Errorf("sweep failed %d task(s), want 0 — the runtime was online before the fail statement ran", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "running" {
		t.Errorf("task status = %q, want running (a task on a recovered runtime was killed)", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 0 {
		t.Errorf("task events = %d, want 0 — no runtime_offline event may be emitted for an online runtime", n)
	}
}

func TestStaleSweepSkipsHeartbeatRefreshedAfterTheTaskLock(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	runtimeID := runtimeIDForAgentTest(t, pool, agentID)

	// Online runtime whose heartbeat has gone stale, with a long-running task: the
	// running branch of the stale predicate, whose liveness signal is last_seen_at.
	if _, err := pool.Exec(ctx,
		`UPDATE agent_runtime SET status = 'online', last_seen_at = now() - interval '30 minutes' WHERE id = $1`,
		runtimeID); err != nil {
		t.Fatalf("stale the heartbeat: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '30 minutes')
		RETURNING id`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	beat := false
	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			rows, e := q.PeekStaleTasksToFail(ctx, db.PeekStaleTasksToFailParams{
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
				MaxPerTick:            100,
			})
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 1)
			for _, r := range rows {
				if util.UUIDToString(r.ID) == taskID {
					mine = append(mine, r)
				}
			}
			if len(mine) != 1 {
				t.Errorf("peek returned %d rows for the seeded task, want 1", len(mine))
			}
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			locked, lErr := qtx.LockStaleTasksByIDsForFail(ctx, db.LockStaleTasksByIDsForFailParams{
				Ids:                   ids,
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
			})
			if lErr != nil {
				return nil, lErr
			}
			if len(locked) != 1 {
				t.Errorf("re-lock returned %d rows, want 1 — the race needs a locked candidate", len(locked))
			}
			// THE RACE: the daemon's heartbeat lands after the task row is locked.
			n, tErr := queries.TouchAgentRuntimeLastSeen(ctx, runtimeID)
			if tErr != nil {
				t.Errorf("touch heartbeat: %v", tErr)
			} else if n != 1 {
				t.Errorf("heartbeat updated %d rows, want 1", n)
			} else {
				beat = true
			}
			return locked, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailStaleTasksByIDs(ctx, db.FailStaleTasksByIDsParams{
				Ids:                   ids,
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
				Error:                 pgtype.Text{String: "task timed out", Valid: true},
				FailureReason:         pgtype.Text{String: "timeout", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("bulk sweep: %v", err)
	}
	if !beat {
		t.Fatal("the heartbeat never landed — the race was not reproduced")
	}

	if len(failed) != 0 {
		t.Errorf("sweep failed %d task(s), want 0 — the daemon proved liveness before the fail statement ran", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "running" {
		t.Errorf("task status = %q, want running (a task whose daemon just heartbeated was killed)", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 0 {
		t.Errorf("task events = %d, want 0 — no timeout event may be emitted for a live daemon", n)
	}
}

// The CAS alone would still leave a sliver: the fail statement's snapshot is taken at
// statement start, so a heartbeat committing between that snapshot and the
// transaction's COMMIT would be invisible and the task would still die. That is why
// the runtime condition is evaluated over a row taken FOR SHARE — the recovery is
// serialized AFTER our decision instead of racing it.
//
// This proves the lock is really held: mid-transaction, once the fail statement has
// run, a real heartbeat on another connection must BLOCK (it gives up on lock_timeout
// rather than committing behind our back), and must succeed once we commit. It also
// pins the `OFFSET 0` fence — without it the planner pushes the liveness test into
// the locked scan, a stale runtime's row matches nothing, no lock is taken, and this
// heartbeat would sail straight through.
func TestStaleFailHoldsTheRuntimeRowUntilCommit(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	runtimeID := runtimeIDForAgentTest(t, pool, agentID)

	if _, err := pool.Exec(ctx,
		`UPDATE agent_runtime SET status = 'online', last_seen_at = now() - interval '30 minutes' WHERE id = $1`,
		runtimeID); err != nil {
		t.Fatalf("stale the heartbeat: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '30 minutes')
		RETURNING id`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	blocked := false
	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(q *db.Queries) ([]db.AgentTaskQueue, error) {
			// Scoped to this test's task: the peek is global, and a sweep that ranged
			// over the whole database would take workspace locks belonging to other
			// packages running against the same DB.
			rows, e := q.PeekStaleTasksToFail(ctx, db.PeekStaleTasksToFailParams{
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
				MaxPerTick:            100,
			})
			if e != nil {
				return nil, e
			}
			mine := make([]db.AgentTaskQueue, 0, 1)
			for _, r := range rows {
				if util.UUIDToString(r.ID) == taskID {
					mine = append(mine, r)
				}
			}
			if len(mine) != 1 {
				t.Errorf("peek returned %d rows for the seeded task, want 1", len(mine))
			}
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.LockStaleTasksByIDsForFail(ctx, db.LockStaleTasksByIDsForFailParams{
				Ids:                   ids,
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
			})
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			rows, fErr := qtx.FailStaleTasksByIDs(ctx, db.FailStaleTasksByIDsParams{
				Ids:                   ids,
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
				Error:                 pgtype.Text{String: "task timed out", Valid: true},
				FailureReason:         pgtype.Text{String: "timeout", Valid: true},
			})
			if fErr != nil {
				return nil, fErr
			}
			// The fail statement has run and this transaction is NOT yet committed.
			// A real heartbeat must not be able to slip in behind our decision.
			blocked = heartbeatBlocksForTest(t, pool, runtimeID)
			return rows, nil
		})
	if err != nil {
		t.Fatalf("bulk sweep: %v", err)
	}
	if !blocked {
		t.Error("a heartbeat committed while the fail transaction was open — the runtime row was not held, so the eligibility check is not atomic with the state change")
	}
	// This task WAS genuinely stale, so it must still be failed exactly once.
	if len(failed) != 1 {
		t.Fatalf("sweep failed %d task(s), want 1", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "failed" {
		t.Errorf("task status = %q, want failed", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 1 {
		t.Errorf("task events = %d, want 1", n)
	}
	// Once we committed, the runtime row is free again — the lock is scoped to the
	// decision, not leaked.
	if _, err := queries.TouchAgentRuntimeLastSeen(ctx, runtimeID); err != nil {
		t.Errorf("heartbeat after commit: %v — the runtime row must be released", err)
	}
}

// heartbeatBlocksForTest runs a REAL heartbeat UPDATE on its own connection under a
// short lock_timeout. It reports true when the statement could not acquire the
// runtime row (55P03), i.e. someone is holding it.
func heartbeatBlocksForTest(t *testing.T, pool *pgxpool.Pool, runtimeID pgtype.UUID) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire heartbeat conn: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin heartbeat tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1 AND status = 'online'`, runtimeID)
	if err == nil {
		return false // it got the row — nothing was holding it
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return true // lock_not_available: the row is held, as intended
	}
	t.Fatalf("heartbeat failed for an unexpected reason: %v", err)
	return false
}
