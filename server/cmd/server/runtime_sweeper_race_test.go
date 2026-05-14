package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestMarkRuntimesOfflineByIDs_RespectsConcurrentHeartbeat is the regression
// test for the SELECT/filter/UPDATE race that GPT-Boy flagged in PR #2121:
// once the sweeper splits the candidate gather and the actual write into two
// statements, a heartbeat that lands between them must veto the offline
// flip. The original single-statement MarkStaleRuntimesOffline preserved
// this implicitly because the predicate and the write lived in one UPDATE;
// MarkRuntimesOfflineByIDs preserves it explicitly via the stale predicate
// re-check.
func TestMarkRuntimesOfflineByIDs_RespectsConcurrentHeartbeat(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	// Insert an "online" runtime whose last_seen_at is well past the stale
	// threshold — the SELECT step would pick this up as a candidate. Use
	// 2× the threshold so this stays correct if staleThresholdSeconds is
	// retuned in the future.
	staleSeed := time.Duration(staleThresholdSeconds*2) * time.Second
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'claude',
			'online', '', '{}'::jsonb, now() - make_interval(secs => $3))
		RETURNING id
	`, testWorkspaceID, "race-test-runtime", staleSeed.Seconds()).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// Simulate the race window: a heartbeat lands between SELECT and UPDATE.
	// The sweeper has already gathered this runtime as a candidate and is
	// about to flip it offline; the heartbeat path bumps last_seen_at to now.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1`,
		runtimeID,
	); err != nil {
		t.Fatalf("simulate concurrent heartbeat: %v", err)
	}

	// The final UPDATE must NOT mark this runtime offline — its last_seen_at
	// is now fresh, so the stale predicate inside MarkRuntimesOfflineByIDs
	// vetoes the write.
	rows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          []pgtype.UUID{parseUUID(runtimeID)},
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("MarkRuntimesOfflineByIDs: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows offlined (stale predicate should veto), got %d", len(rows))
	}

	var status string
	var lastSeen time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT status, last_seen_at FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&status, &lastSeen); err != nil {
		t.Fatalf("read back runtime: %v", err)
	}
	if status != "online" {
		t.Fatalf("runtime was incorrectly marked offline despite fresh heartbeat: status=%q", status)
	}
	if time.Since(lastSeen) > 30*time.Second {
		t.Fatalf("last_seen_at not preserved: %s ago", time.Since(lastSeen))
	}
}

// TestMarkRuntimesOfflineByIDs_OfflinesGenuinelyStale confirms the happy
// path still works: a runtime whose last_seen_at really is past the stale
// threshold gets marked offline by the same query.
func TestMarkRuntimesOfflineByIDs_OfflinesGenuinelyStale(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	staleSeed := time.Duration(staleThresholdSeconds*2) * time.Second
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'claude',
			'online', '', '{}'::jsonb, now() - make_interval(secs => $3))
		RETURNING id
	`, testWorkspaceID, "race-test-stale-runtime", staleSeed.Seconds()).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	rows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          []pgtype.UUID{parseUUID(runtimeID)},
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("MarkRuntimesOfflineByIDs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row offlined, got %d", len(rows))
	}

	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&status); err != nil {
		t.Fatalf("read back runtime: %v", err)
	}
	if status != "offline" {
		t.Fatalf("genuinely-stale runtime not marked offline: status=%q", status)
	}
}
