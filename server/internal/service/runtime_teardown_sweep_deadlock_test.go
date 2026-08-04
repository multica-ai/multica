package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The bulk sweepers walk agent_task_queue -> agent_runtime: they lock the candidate
// task rows and then take FOR SHARE on each task's runtime row, because that is where
// their eligibility actually lives. The runtime and runtime-profile teardowns walk the
// SAME two tables in the OPPOSITE order: FOR UPDATE on the runtime row(s) first
// (LockAgentRuntime / ListAgentRuntimeIDsByProfile), then CancelAgentTasksByRuntimeOrAgent
// on that runtime's tasks. Two paths taking two tables in opposite orders is a real
// deadlock cycle (40P01) — PostgreSQL kills one side, surfacing either a 500 on the
// delete or a rolled-back sweep tick that the user has to wait out.
//
// Neither walk can simply be inverted; both orders are load-bearing where they are. So
// the two paths are made mutually exclusive one level up, on the workspace row that is
// already the automation mutex: teardown takes LockWorkspaceForRuntimeTeardown
// (FOR UPDATE) FIRST, which conflicts with the FOR KEY SHARE the sweep takes on the
// same row. A sweep then skips a workspace under teardown (its TryLock is SKIP LOCKED)
// and a teardown waits for an in-flight sweep before touching any runtime row, so
// neither can hold one table while waiting for the other.
//
// Both tests below drive the REAL production query sequences on both sides and
// deliberately interleave them at the point the cycle would close. Drop the workspace
// lock from the teardown side and each one reproduces 40P01.

// heldTeardown is one side of the race: a teardown transaction parked after it has
// taken its first locks, so the sweep runs while it is held.
type heldTeardown struct {
	tx   pgx.Tx
	qtx  *db.Queries
	conn *pgxpool.Conn
}

func (h *heldTeardown) release(ctx context.Context) {
	h.tx.Rollback(ctx)
	h.conn.Release()
}

// beginHeldTeardown opens a transaction and runs `lock`, the production lock prefix of
// a teardown, leaving the transaction open and holding those locks.
func beginHeldTeardown(t *testing.T, pool *pgxpool.Pool, lock func(*db.Queries) error) *heldTeardown {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire teardown conn: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		t.Fatalf("begin teardown tx: %v", err)
	}
	held := &heldTeardown{tx: tx, qtx: db.New(pool).WithTx(tx), conn: conn}
	if err := lock(held.qtx); err != nil {
		held.release(ctx)
		t.Fatalf("teardown lock prefix: %v", err)
	}
	return held
}

// seedSweepCandidate creates a workspace whose runtime is offline with a task still in
// flight — a genuine candidate for the offline-runtime sweep.
func seedSweepCandidate(t *testing.T, pool *pgxpool.Pool) (agentID, issueID, taskID string, runtimeID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	_, _, agentID, issueID = seedAttributionFixture(t, pool)
	runtimeID = runtimeIDForAgentTest(t, pool, agentID)
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("take runtime offline: %v", err)
	}
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
	return agentID, issueID, taskID, runtimeID
}

// runOfflineSweep runs the production offline-runtime sweep, scoped to one task.
func runOfflineSweep(ctx context.Context, svc *TaskService, taskID string) ([]db.AgentTaskQueue, error) {
	return svc.FailBulkTasksWithEvents(ctx,
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
			return mine, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.LockOfflineRuntimeTasksByIDsForFail(ctx, ids)
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailOfflineRuntimeTasksByIDs(ctx, db.FailOfflineRuntimeTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "runtime went offline", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			})
		})
}

// runStaleSweep runs the production stale sweep, scoped to one task.
func runStaleSweep(ctx context.Context, svc *TaskService, taskID string) ([]db.AgentTaskQueue, error) {
	return svc.FailBulkTasksWithEvents(ctx,
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
			return qtx.FailStaleTasksByIDs(ctx, db.FailStaleTasksByIDsParams{
				Ids:                   ids,
				DispatchedTimeoutSecs: 60,
				RunningTimeoutSecs:    60,
				RuntimeStaleSecs:      60,
				Error:                 pgtype.Text{String: "task timed out", Valid: true},
				FailureReason:         pgtype.Text{String: "timeout", Valid: true},
			})
		})
}

// raceTeardownAgainstSweep parks a teardown on its first locks, runs the sweep
// concurrently, then lets the teardown reach the task cancel that closes the cycle.
// It fails the test if EITHER side deadlocks.
func raceTeardownAgainstSweep(
	t *testing.T,
	held *heldTeardown,
	runtimeID pgtype.UUID,
	sweep func() error,
) {
	t.Helper()
	ctx := context.Background()
	defer held.release(ctx)

	swept := make(chan error, 1)
	go func() { swept <- sweep() }()

	// Let the sweep get as far as it can: past its workspace lock and onto the task
	// rows, so it is already waiting on the runtime row when the teardown asks for the
	// tasks. That is exactly where the cycle closes without the workspace mutex.
	time.Sleep(700 * time.Millisecond)

	_, cancelErr := held.qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{runtimeID},
		AgentIds:   nil,
	})
	assertNoDeadlock(t, cancelErr, "runtime teardown (cancel tasks)")

	select {
	case err := <-swept:
		assertNoDeadlock(t, err, "bulk sweep")
	case <-time.After(15 * time.Second):
		t.Fatal("the sweep never returned — it is stuck behind the teardown's runtime lock " +
			"instead of skipping the workspace (the teardown mutex is not excluding it)")
	}
}

// Direct runtime delete (runtime.go: LockAgentRuntime then the task teardown) racing
// the offline-runtime sweep.
func TestRuntimeDeleteRacingOfflineSweepDoesNotDeadlock(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	agentID, _, taskID, runtimeID := seedSweepCandidate(t, pool)
	workspaceID := workspaceIDForAgentTest(t, pool, agentID)

	// The production lock prefix of DeleteAgentRuntime: workspace mutex, then the
	// runtime row.
	held := beginHeldTeardown(t, pool, func(qtx *db.Queries) error {
		if _, err := qtx.LockWorkspaceForRuntimeTeardown(ctx, workspaceID); err != nil {
			return fmt.Errorf("lock workspace: %w", err)
		}
		if _, err := qtx.LockAgentRuntime(ctx, runtimeID); err != nil {
			return fmt.Errorf("lock runtime: %w", err)
		}
		return nil
	})

	raceTeardownAgainstSweep(t, held, runtimeID, func() error {
		_, err := runOfflineSweep(ctx, svc, taskID)
		return err
	})
}

// Runtime PROFILE delete (runtime_profile.go: LockRuntimeProfileForDelete then
// ListAgentRuntimeIDsByProfile FOR UPDATE, then the same task teardown) racing the
// stale sweep — the second entry point into the reverse order.
func TestRuntimeProfileDeleteRacingStaleSweepDoesNotDeadlock(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	agentID, _, taskID, runtimeID := seedSweepCandidate(t, pool)
	workspaceID := workspaceIDForAgentTest(t, pool, agentID)

	// The stale sweep's running branch needs an ONLINE runtime whose heartbeat has
	// gone stale (the offline seed leaves it offline).
	if _, err := pool.Exec(ctx,
		`UPDATE agent_runtime SET status = 'online', last_seen_at = now() - interval '30 minutes' WHERE id = $1`,
		runtimeID); err != nil {
		t.Fatalf("stale the heartbeat: %v", err)
	}

	// Bind the runtime to a profile so the profile cascade actually enumerates it.
	var profileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO runtime_profile (workspace_id, display_name, protocol_family, command_name)
		VALUES ($1, 'teardown-race-profile', 'codex', 'codex')
		RETURNING id`, util.UUIDToString(workspaceID)).Scan(&profileID); err != nil {
		t.Fatalf("seed runtime profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET profile_id = $1 WHERE id = $2`, profileID, runtimeID); err != nil {
		t.Fatalf("bind runtime to profile: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE agent_runtime SET profile_id = NULL WHERE id = $1`, runtimeID)
		pool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, profileID)
	})

	// The production lock prefix of DeleteRuntimeProfile: workspace mutex, profile,
	// then every runtime bound to it.
	held := beginHeldTeardown(t, pool, func(qtx *db.Queries) error {
		if _, err := qtx.LockWorkspaceForRuntimeTeardown(ctx, workspaceID); err != nil {
			return fmt.Errorf("lock workspace: %w", err)
		}
		if _, err := qtx.LockRuntimeProfileForDelete(ctx, db.LockRuntimeProfileForDeleteParams{
			ID:          util.MustParseUUID(profileID),
			WorkspaceID: workspaceID,
		}); err != nil {
			return fmt.Errorf("lock profile: %w", err)
		}
		ids, err := qtx.ListAgentRuntimeIDsByProfile(ctx, db.ListAgentRuntimeIDsByProfileParams{
			ProfileID:   util.MustParseUUID(profileID),
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return fmt.Errorf("lock profile runtimes: %w", err)
		}
		if len(ids) != 1 {
			return fmt.Errorf("profile enumerated %d runtimes, want 1", len(ids))
		}
		return nil
	})

	raceTeardownAgainstSweep(t, held, runtimeID, func() error {
		_, err := runStaleSweep(ctx, svc, taskID)
		return err
	})
}

// workspaceIDForAgentTest resolves an agent's workspace, so a test can drive the
// workspace-level teardown mutex the production handlers take.
func workspaceIDForAgentTest(t *testing.T, pool *pgxpool.Pool, agentID string) pgtype.UUID {
	t.Helper()
	var workspaceID string
	if err := pool.QueryRow(context.Background(),
		`SELECT workspace_id FROM agent WHERE id = $1`, agentID).Scan(&workspaceID); err != nil {
		t.Fatalf("load workspace id: %v", err)
	}
	return util.MustParseUUID(workspaceID)
}
