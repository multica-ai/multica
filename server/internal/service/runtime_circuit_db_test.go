package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRuntimeProviderCircuitLifecycle pins the DB-level invariants the terminal
// task callbacks (openRuntimeProviderCircuitTx / syncRuntimeCircuitOnSuccess)
// rely on, against a real database:
//
//   - a fresh quota failure opens the circuit at generation 1;
//   - a newer failure escalates it and bumps the generation;
//   - a stale (older) failure callback is a no-op, not a timer reset;
//   - a stale success cannot close a fresher open circuit;
//   - a genuine newer success closes it;
//   - once reset_at has passed, exactly one half-open probe lease is granted.
//
// The generation/stale ordering is what makes duplicate and out-of-order daemon
// callbacks safe (invariants I3, I6).
func TestRuntimeProviderCircuitLifecycle(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	q := db.New(pool)

	suffix := time.Now().UnixNano()
	userID := insertBindingTeardownRow(t, pool, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Circuit LC", fmt.Sprintf("circuit-lc-%d@multica.ai", suffix))
	workspaceID := insertBindingTeardownRow(t, pool, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'CLC') RETURNING id`,
		"Circuit LC", fmt.Sprintf("circuit-lc-%d", suffix))
	execBindingTeardown(t, pool, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	runtimeID := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "Circuit Runtime")
	runtime := util.MustParseUUID(runtimeID)
	workspace := util.MustParseUUID(workspaceID)
	const provider = "binding_teardown_test"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID)
	})

	ts := func(base time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: base, Valid: true} }
	base := time.Now().UTC().Truncate(time.Second)

	// 1. Fresh quota failure opens the circuit at generation 1.
	firstFail := ts(base.Add(-2 * time.Hour))
	firstTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	opened, err := q.OpenRuntimeProviderCircuit(ctx, db.OpenRuntimeProviderCircuitParams{
		WorkspaceID:        workspace,
		RuntimeID:          runtime,
		Provider:           provider,
		Reason:             pgtype.Text{String: circuitClassQuota, Valid: true},
		ResetAt:            ts(base.Add(-time.Minute)), // already elapsed, so a probe is grantable below
		FailureCompletedAt: firstFail,
		FailureTaskID:      firstTask,
	})
	if err != nil {
		t.Fatalf("open circuit: %v", err)
	}
	if len(opened) != 1 || opened[0].State != "open" || opened[0].Generation != 1 {
		t.Fatalf("first open: rows=%d state=%q gen=%d, want 1/open/1", len(opened), rowState(opened), rowGen(opened))
	}

	// 2. A newer failure escalates the open circuit and bumps the generation.
	secondFail := ts(base.Add(-30 * time.Minute))
	secondTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	escalated, err := q.OpenRuntimeProviderCircuit(ctx, db.OpenRuntimeProviderCircuitParams{
		WorkspaceID:        workspace,
		RuntimeID:          runtime,
		Provider:           provider,
		Reason:             pgtype.Text{String: circuitClassAuth, Valid: true},
		ResetAt:            ts(base.Add(-time.Minute)),
		FailureCompletedAt: secondFail,
		FailureTaskID:      secondTask,
	})
	if err != nil {
		t.Fatalf("escalate circuit: %v", err)
	}
	if len(escalated) != 1 || escalated[0].Generation != 2 {
		t.Fatalf("escalation: rows=%d gen=%d, want 1 row / gen 2", len(escalated), rowGen(escalated))
	}

	// 3. A stale (older) failure callback is a no-op: no row, generation unchanged.
	staleFail := ts(base.Add(-3 * time.Hour))
	staleTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	stale, err := q.OpenRuntimeProviderCircuit(ctx, db.OpenRuntimeProviderCircuitParams{
		WorkspaceID:        workspace,
		RuntimeID:          runtime,
		Provider:           provider,
		Reason:             pgtype.Text{String: circuitClassQuota, Valid: true},
		ResetAt:            ts(base.Add(time.Hour)),
		FailureCompletedAt: staleFail,
		FailureTaskID:      staleTask,
	})
	if err != nil {
		t.Fatalf("stale open: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale failure callback wrote %d rows, want 0 (no-op)", len(stale))
	}
	if gen := scanInt(t, pool, `SELECT generation FROM runtime_provider_circuit WHERE runtime_id = $1 AND provider = $2`, runtimeID, provider); gen != 2 {
		t.Fatalf("generation after stale callback = %d, want 2 (unchanged)", gen)
	}

	// 4. A stale success (completed before the current failure) cannot close it.
	staleSuccess := ts(base.Add(-time.Hour)) // older than secondFail (-30m)
	staleSuccessTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	closedStale, err := q.CloseRuntimeProviderCircuitOnSuccess(ctx, db.CloseRuntimeProviderCircuitOnSuccessParams{
		RuntimeID:          runtime,
		Provider:           provider,
		SuccessCompletedAt: staleSuccess,
		SuccessTaskID:      staleSuccessTask,
	})
	if err != nil {
		t.Fatalf("stale close: %v", err)
	}
	if len(closedStale) != 0 {
		t.Fatalf("stale success closed the circuit (%d rows), want 0", len(closedStale))
	}
	if st := scanString(t, pool, `SELECT state FROM runtime_provider_circuit WHERE runtime_id = $1 AND provider = $2`, runtimeID, provider); st != "open" {
		t.Fatalf("state after stale success = %q, want open", st)
	}

	// 5. A half-open probe lease is granted exactly once while it is unexpired.
	probeTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	probe, err := q.AcquireRuntimeProviderHalfOpenProbe(ctx, db.AcquireRuntimeProviderHalfOpenProbeParams{
		RuntimeID:      runtime,
		Provider:       provider,
		ProbeTaskID:    probeTask,
		ProbeExpiresAt: ts(base.Add(10 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("acquire probe: %v", err)
	}
	if len(probe) != 1 || probe[0].State != "half_open" {
		t.Fatalf("first probe: rows=%d state=%q, want 1/half_open", len(probe), rowState(probe))
	}
	secondProbeTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	probe2, err := q.AcquireRuntimeProviderHalfOpenProbe(ctx, db.AcquireRuntimeProviderHalfOpenProbeParams{
		RuntimeID:      runtime,
		Provider:       provider,
		ProbeTaskID:    secondProbeTask,
		ProbeExpiresAt: ts(base.Add(10 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("acquire second probe: %v", err)
	}
	if len(probe2) != 0 {
		t.Fatalf("second probe acquired a lease (%d rows) while the first is unexpired, want 0", len(probe2))
	}

	// 6. A genuine newer success closes the circuit.
	freshSuccess := ts(base) // newer than secondFail (-30m)
	freshSuccessTask := util.MustParseUUID(insertBindingTeardownRow(t, pool, `SELECT gen_random_uuid()`))
	closed, err := q.CloseRuntimeProviderCircuitOnSuccess(ctx, db.CloseRuntimeProviderCircuitOnSuccessParams{
		RuntimeID:          runtime,
		Provider:           provider,
		SuccessCompletedAt: freshSuccess,
		SuccessTaskID:      freshSuccessTask,
	})
	if err != nil {
		t.Fatalf("fresh close: %v", err)
	}
	if len(closed) != 1 || closed[0].State != "closed" {
		t.Fatalf("fresh success: rows=%d state=%q, want 1/closed", len(closed), rowState(closed))
	}
	if closed[0].ResetAt.Valid || closed[0].ProbeTaskID.Valid {
		t.Fatalf("closed circuit still carries reset_at/probe: %+v", closed[0])
	}
}

func rowState(rows []db.RuntimeProviderCircuit) string {
	if len(rows) == 0 {
		return "<none>"
	}
	return rows[0].State
}

func rowGen(rows []db.RuntimeProviderCircuit) int64 {
	if len(rows) == 0 {
		return -1
	}
	return rows[0].Generation
}
