package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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
