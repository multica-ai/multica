package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const issueLifecycleMigrationTestSchema = "issue_lifecycle_migration_test"

func TestIssueLifecycleMigrationsBackfillIdempotentlyAndRollBack(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire Postgres connection: %v", err)
	}
	defer conn.Release()

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+issueLifecycleMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+issueLifecycleMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, issueLifecycleMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE workspace (id UUID PRIMARY KEY);
		CREATE TABLE project (id UUID PRIMARY KEY, workspace_id UUID NOT NULL);
		CREATE TABLE issue (
			id UUID PRIMARY KEY, workspace_id UUID NOT NULL, status TEXT NOT NULL,
			revision BIGINT NOT NULL DEFAULT 1, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE issue_status (
			workspace_id UUID NOT NULL, key TEXT NOT NULL, name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '', color TEXT NOT NULL, position DOUBLE PRECISION NOT NULL,
			category TEXT NOT NULL, archived_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE agent_task_queue (id UUID PRIMARY KEY, status TEXT NOT NULL DEFAULT 'queued');
	`); err != nil {
		t.Fatalf("create legacy fixture schema: %v", err)
	}

	const (
		workspaceID = "70220000-0000-4000-8000-000000000001"
		projectID   = "70220000-0000-4000-8000-000000000002"
		issueID     = "70220000-0000-4000-8000-000000000003"
	)
	if _, err := conn.Exec(ctx, `INSERT INTO workspace (id) VALUES ($1)`, workspaceID); err != nil {
		t.Fatalf("seed legacy workspace: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO project (id, workspace_id) VALUES ($1, $2)`, projectID, workspaceID); err != nil {
		t.Fatalf("seed legacy project: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, workspace_id, status, revision) VALUES ($1, $2, 'human_review', 7)`, issueID, workspaceID); err != nil {
		t.Fatalf("seed legacy issue: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO issue_status (workspace_id, key, name, color, position, category) VALUES
			($1, 'backlog', 'Backlog', '#6b7280', 0, 'backlog'),
			($1, 'todo', 'Todo', '#6b7280', 1, 'todo'),
			($1, 'in_progress', 'In Progress', '#6b7280', 2, 'in_progress'),
			($1, 'in_review', 'In Review', '#6b7280', 3, 'in_review'),
			($1, 'human_review', 'Human Review', '#6b7280', 4, 'in_review'),
			($1, 'done', 'Done', '#6b7280', 5, 'done'),
			($1, 'blocked', 'Blocked', '#6b7280', 6, 'blocked'),
			($1, 'cancelled', 'Cancelled', '#6b7280', 7, 'cancelled');
	`, workspaceID); err != nil {
		t.Fatalf("seed legacy status catalog: %v", err)
	}

	up := []string{
		"451_issue_lifecycle_foundation.up.sql",
		"452_issue_lifecycle_pkey_index.up.sql",
		"453_issue_lifecycle_status_pkey_index.up.sql",
		"454_issue_transition_pkey_index.up.sql",
		"455_automation_execution_pkey_index.up.sql",
		"456_issue_lifecycle_primary_keys.up.sql",
		"457_issue_lifecycle_scope_index.up.sql",
		"458_issue_lifecycle_legacy_status_index.up.sql",
		"459_issue_transition_revision_index.up.sql",
		"460_automation_execution_trigger_index.up.sql",
		"461_issue_transition_timeline_index.up.sql",
		"462_issue_lifecycle_binding_index.up.sql",
		"463_agent_task_automation_execution_index.up.sql",
		"464_issue_lifecycle_backfill.up.sql",
		"465_automation_execution_task_status.up.sql",
	}
	for _, name := range up {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}
	// The backfill itself is explicitly restartable after partial operator runs.
	applyMigrationFile(t, ctx, conn.Conn(), "464_issue_lifecycle_backfill.up.sql")

	assertLifecycleMigrationCount(t, ctx, conn, "issue_lifecycle", 1)
	assertLifecycleMigrationCount(t, ctx, conn, "issue_lifecycle_status", 8)
	assertLifecycleMigrationCount(t, ctx, conn, "issue_transition", 1)
	var phase, legacyStatus, cause string
	var issueBound, projectInherits bool
	if err := conn.QueryRow(ctx, `
		SELECT s.phase, s.legacy_status_key, t.cause,
		       i.lifecycle_id IS NOT NULL AND i.lifecycle_status_id = s.id AND i.last_transition_id = t.id,
		       p.default_issue_lifecycle_id IS NULL
		FROM issue i
		JOIN issue_lifecycle_status s ON s.id = i.lifecycle_status_id
		JOIN issue_transition t ON t.id = i.last_transition_id
		JOIN project p ON p.id = $2
		WHERE i.id = $1
	`, issueID, projectID).Scan(&phase, &legacyStatus, &cause, &issueBound, &projectInherits); err != nil {
		t.Fatalf("read lifecycle backfill: %v", err)
	}
	if phase != "started" || legacyStatus != "human_review" || cause != "migration_backfill" || !issueBound || !projectInherits {
		t.Fatalf("backfill = phase %q status %q cause %q issue_bound=%v project_inherits=%v", phase, legacyStatus, cause, issueBound, projectInherits)
	}
	var orderedStatuses []string
	var orderedPositions []float64
	if err := conn.QueryRow(ctx, `
		SELECT array_agg(legacy_status_key ORDER BY position), array_agg(position ORDER BY position)
		FROM issue_lifecycle_status
	`).Scan(&orderedStatuses, &orderedPositions); err != nil {
		t.Fatalf("read lifecycle status order: %v", err)
	}
	wantOrder := []string{"backlog", "todo", "in_progress", "in_review", "human_review", "done", "blocked", "cancelled"}
	for i := range wantOrder {
		if orderedStatuses[i] != wantOrder[i] || orderedPositions[i] != float64(i) {
			t.Fatalf("lifecycle status[%d] = key %q position %v, want key %q position %d", i, orderedStatuses[i], orderedPositions[i], wantOrder[i], i)
		}
	}

	down := []string{
		"465_automation_execution_task_status.down.sql",
		"464_issue_lifecycle_backfill.down.sql",
		"463_agent_task_automation_execution_index.down.sql",
		"462_issue_lifecycle_binding_index.down.sql",
		"461_issue_transition_timeline_index.down.sql",
		"460_automation_execution_trigger_index.down.sql",
		"459_issue_transition_revision_index.down.sql",
		"458_issue_lifecycle_legacy_status_index.down.sql",
		"457_issue_lifecycle_scope_index.down.sql",
		"456_issue_lifecycle_primary_keys.down.sql",
		"455_automation_execution_pkey_index.down.sql",
		"454_issue_transition_pkey_index.down.sql",
		"453_issue_lifecycle_status_pkey_index.down.sql",
		"452_issue_lifecycle_pkey_index.down.sql",
		"451_issue_lifecycle_foundation.down.sql",
	}
	for _, name := range down {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}
	var lifecycleExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, issueLifecycleMigrationTestSchema+".issue_lifecycle").Scan(&lifecycleExists); err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
	if lifecycleExists {
		t.Fatal("issue_lifecycle still exists after down migrations")
	}
}

func assertLifecycleMigrationCount(t *testing.T, ctx context.Context, conn *pgxpool.Conn, table string, want int) {
	t.Helper()
	var got int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
