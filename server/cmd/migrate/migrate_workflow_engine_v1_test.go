package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkflowEngineV1Schema(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	for _, table := range []string{"workflow_definition", "workflow_run", "workflow_transition"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s missing", table)
		}
	}

	assertWorkflowIndex(t, ctx, pool, "workflow_run_one_active_per_issue", "status", "blocked_materialization")
	assertWorkflowConstraint(t, ctx, pool, "workflow_definition", "UNIQUE (workspace_id, name, version)")
	assertWorkflowConstraint(t, ctx, pool, "workflow_transition", "UNIQUE (workflow_run_id, idempotency_key)")
	assertWorkflowConstraint(t, ctx, pool, "workflow_run", "status")
	assertWorkflowConstraint(t, ctx, pool, "workflow_run", "current_stage")
}

func assertWorkflowIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, fragments ...string) {
	t.Helper()
	var definition string
	if err := pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`, name).Scan(&definition); err != nil {
		t.Fatalf("index %s missing: %v", name, err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("index %s = %q, want fragment %q", name, definition, fragment)
		}
	}
}

func assertWorkflowConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, fragment string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class r ON r.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = r.relnamespace
		WHERE n.nspname = 'public' AND r.relname = $1`, table)
	if err != nil {
		t.Fatalf("constraints for %s: %v", table, err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			t.Fatalf("scan constraint for %s: %v", table, err)
		}
		if strings.Contains(definition, fragment) {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate constraints for %s: %v", table, err)
	}
	if !found {
		t.Fatalf("constraint on %s missing fragment %q", table, fragment)
	}
}
