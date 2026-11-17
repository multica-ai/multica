package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStaleDispatchRecoveryHonorsBatchLimitWithStaleStatistics(t *testing.T) {
	for _, limit := range []int32{0, 1, 2, 10} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := sharedTestPool(t).Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			// Copy the real schemas so statistics and claims stay local to this test.
			for _, name := range []string{"agent", "agent_runtime", "agent_task_queue"} {
				table := pgx.Identifier{name}.Sanitize()
				if _, err := tx.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL) ON COMMIT DROP"); err != nil {
					t.Fatal(err)
				}
			}
			var runtimeID, agentID pgtype.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, visibility, last_seen_at)
				VALUES (gen_random_uuid(), 'batch reclaim', 'local', 'multica_daemon', 'online', 'public', now())
				RETURNING id
			`).Scan(&runtimeID); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
				SELECT workspace_id, 'batch reclaim', 'local', id FROM agent_runtime WHERE id = $1
				RETURNING id
			`, runtimeID).Scan(&agentID); err != nil {
				t.Fatal(err)
			}
			const seed = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, dispatched_at, priority)
				SELECT $1, $2, 'dispatched', now() - interval '10 minutes', 6 - n
				FROM generate_series($3::int, $4::int) n`
			if _, err := tx.Exec(ctx, seed, agentID, runtimeID, 1, 1); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "ANALYZE agent; ANALYZE agent_runtime; ANALYZE agent_task_queue"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, seed, agentID, runtimeID, 2, 5); err != nil {
				t.Fatal(err)
			}
			queries := db.New(tx)
			params := db.ReclaimStaleDispatchedTasksForRuntimesParams{
				RuntimeIds: []pgtype.UUID{runtimeID}, PrepareLeaseSecs: 60,
				ClaimRecoverySecs: 30, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds, MaxTasks: limit,
			}
			claimed := 0
			for {
				batch, err := queries.ReclaimStaleDispatchedTasksForRuntimes(ctx, params)
				if err != nil {
					t.Fatal(err)
				}
				want := min(int(limit), 5-claimed)
				if len(batch) != want {
					t.Fatalf("reclaimed %d tasks, want %d (limit %d, already claimed %d)", len(batch), want, limit, claimed)
				}
				for _, task := range batch {
					if task.Priority > int32(5-claimed) || task.Priority <= int32(5-claimed-want) {
						t.Fatalf("reclaimed priority %d outside the next %d highest-priority tasks", task.Priority, want)
					}
				}
				claimed += len(batch)
				var renewed int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE prepare_lease_expires_at > now()`).Scan(&renewed); err != nil {
					t.Fatal(err)
				}
				if renewed != claimed {
					t.Fatalf("renewed %d task leases after returning %d tasks", renewed, claimed)
				}
				if want == 0 {
					break
				}
			}
		})
	}
}

// TestRuntimeHealthGatesClaimsAndStaleDispatchRecovery directly covers every
// SQL release path that can hand work to a daemon. Offline, stale, or wrong
// runtimes must leave the row untouched; a fresh scoped runtime may claim or
// recover the exact task.
func TestRuntimeHealthGatesClaimsAndStaleDispatchRecovery(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID)
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert queued task: %v", err)
	}

	claim := func(runtime string) (db.AgentTaskQueue, error) {
		return q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
			AgentID:          util.MustParseUUID(agentID),
			RuntimeID:        util.MustParseUUID(runtime),
			PrepareLeaseSecs: 60,
			RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
		})
	}
	assertNoClaim := func(label, runtime string) {
		t.Helper()
		if _, err := claim(runtime); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s: ClaimAgentTask error = %v, want no rows", label, err)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
			t.Fatalf("%s: read task status: %v", label, err)
		}
		if status != "queued" {
			t.Fatalf("%s: task status = %q, want queued", label, status)
		}
	}

	assertNoClaim("wrong runtime scope", "00000000-0000-0000-0000-000000000001")
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("mark runtime offline: %v", err)
	}
	assertNoClaim("offline runtime", runtimeID)

	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() - interval '10 minutes' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("make runtime heartbeat stale: %v", err)
	}
	assertNoClaim("stale runtime heartbeat", runtimeID)

	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("refresh runtime heartbeat: %v", err)
	}
	claimed, err := claim(runtimeID)
	if err != nil {
		t.Fatalf("fresh runtime claim: %v", err)
	}
	if util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), taskID)
	}

	resetStaleDispatch := func() {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE agent_task_queue
			SET status = 'dispatched', started_at = NULL,
			    dispatched_at = now() - interval '10 minutes',
			    prepare_lease_expires_at = now() - interval '1 minute'
			WHERE id = $1
		`, taskID); err != nil {
			t.Fatalf("reset stale dispatch: %v", err)
		}
	}
	reclaimOne := func() (db.AgentTaskQueue, error) {
		return q.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
			RuntimeID:         util.MustParseUUID(runtimeID),
			PrepareLeaseSecs:  60,
			ClaimRecoverySecs: 30,
			RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
		})
	}

	resetStaleDispatch()
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("mark runtime offline before singular reclaim: %v", err)
	}
	if _, err := reclaimOne(); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("offline singular reclaim error = %v, want no rows", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() - interval '10 minutes' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("make runtime stale before singular reclaim: %v", err)
	}
	if _, err := reclaimOne(); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale singular reclaim error = %v, want no rows", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("refresh runtime before singular reclaim: %v", err)
	}
	reclaimed, err := reclaimOne()
	if err != nil {
		t.Fatalf("fresh singular reclaim: %v", err)
	}
	if util.UUIDToString(reclaimed.ID) != taskID {
		t.Fatalf("singular reclaimed task = %s, want %s", util.UUIDToString(reclaimed.ID), taskID)
	}

	resetStaleDispatch()
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("mark runtime offline before batch reclaim: %v", err)
	}
	batchParams := db.ReclaimStaleDispatchedTasksForRuntimesParams{
		RuntimeIds:        []pgtype.UUID{util.MustParseUUID(runtimeID)},
		PrepareLeaseSecs:  60,
		ClaimRecoverySecs: 30,
		RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
		MaxTasks:          10,
	}
	batch, err := q.ReclaimStaleDispatchedTasksForRuntimes(ctx, batchParams)
	if err != nil {
		t.Fatalf("offline batch reclaim: %v", err)
	}
	if len(batch) != 0 {
		t.Fatalf("offline batch reclaimed %d tasks, want 0", len(batch))
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("refresh runtime before batch reclaim: %v", err)
	}
	batch, err = q.ReclaimStaleDispatchedTasksForRuntimes(ctx, batchParams)
	if err != nil {
		t.Fatalf("fresh batch reclaim: %v", err)
	}
	if len(batch) != 1 || util.UUIDToString(batch[0].ID) != taskID {
		t.Fatalf("batch reclaimed %d tasks, want task %s", len(batch), taskID)
	}
}
