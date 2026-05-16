package runtime

// JEH-1475: regression test — a rate-limit failure on Runtime A must not
// cause Runtime B to become paused. Tests the "pause is always scoped to the
// failing runtime" invariant end-to-end through MaybeAutoPauseOnFailure.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestMaybeAutoPauseOnFailure_OnlyPausesFailingRuntime is the JEH-1475
// invariant test: when Runtime A fails with a rate-limit error, Runtime B
// must remain unpaused. Uses the shared TestMain DB fixture.
func TestMaybeAutoPauseOnFailure_OnlyPausesFailingRuntime(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping pause-scope integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	wsID := runtimeAccountTestWSID
	userID := runtimeAccountTestUserID

	// Create two extra runtimes in the shared test workspace.
	var runtimeA, runtimeB pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1, $2, $3, 'local', 'claude', 'online', '', $4)
		RETURNING id`,
		wsID, "pause-scope-test-daemon-A", "Pause Scope Test A", userID,
	).Scan(&runtimeA); err != nil {
		t.Fatalf("create runtime A: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1, $2, $3, 'local', 'claude', 'online', '', $4)
		RETURNING id`,
		wsID, "pause-scope-test-daemon-B", "Pause Scope Test B", userID,
	).Scan(&runtimeB); err != nil {
		t.Fatalf("create runtime B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM agent_runtime WHERE id = $1 OR id = $2`, runtimeA, runtimeB)
	})

	svc := &Service{
		Cerebro: cerebrodb.New(pool),
		// TaskSvc nil is safe: SuspendActiveTasksForRuntime returns empty
		// (no dispatched/running tasks), so HandleFailedTasks is never called.
		TaskSvc: nil,
		Bus:     nil, // publishRuntimePaused guards: if s.Bus == nil { return }
	}

	// Build a task belonging to Runtime A with a rate-limit error. ID is zero
	// UUID — ReclassifyAsRateLimit is :exec with a WHERE guard, so it's a
	// no-op and returns no error when no row matches.
	taskA := db.AgentTaskQueue{
		RuntimeID: runtimeA,
		Error:     pgtype.Text{String: "You've hit your org's monthly usage limit", Valid: true},
	}

	paused := svc.MaybeAutoPauseOnFailure(ctx, taskA)
	if !paused {
		t.Fatal("MaybeAutoPauseOnFailure returned false — expected true for rate-limit error")
	}

	// Runtime A must be paused.
	var aPausedAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx,
		`SELECT paused_at FROM agent_runtime WHERE id = $1`, runtimeA,
	).Scan(&aPausedAt); err != nil {
		t.Fatalf("read runtime A paused_at: %v", err)
	}
	if !aPausedAt.Valid {
		t.Error("runtime A paused_at is NULL — expected runtime A to be paused")
	}

	// Runtime B must remain unpaused.
	var bPausedAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx,
		`SELECT paused_at FROM agent_runtime WHERE id = $1`, runtimeB,
	).Scan(&bPausedAt); err != nil {
		t.Fatalf("read runtime B paused_at: %v", err)
	}
	if bPausedAt.Valid {
		t.Errorf("runtime B paused_at = %s — expected it to remain unpaused",
			bPausedAt.Time.Format(time.RFC3339))
	}
}
