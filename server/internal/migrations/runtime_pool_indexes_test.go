package migrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
)

func TestRuntimePoolIndexMigrationContracts(t *testing.T) {
	dir := realMigrationsDir(t)
	testCases := []struct {
		file      string
		fragments []string
		forbidden []string
	}{
		{
			file: "272_pending_issue_agent_pool_v3.up.sql",
			fragments: []string{
				"CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_one_pending_task_per_issue_agent_v3",
				"ON agent_task_queue (issue_id, agent_id)",
				"status IN ('queued', 'dispatched')",
				"status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'",
				"status = 'waiting_runtime'",
			},
		},
		{
			file:      "272_pending_issue_agent_pool_v3.down.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent_v3"},
		},
		{
			file:      "273_drop_pending_issue_agent_v2.up.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent_v2"},
		},
		{
			file: "273_drop_pending_issue_agent_v2.down.sql",
			fragments: []string{
				"CREATE UNIQUE INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent_v2",
				"ON agent_task_queue (issue_id, agent_id)",
				"status IN ('queued', 'dispatched')",
				"status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'",
			},
			forbidden: []string{"IF NOT EXISTS", "waiting_runtime"},
		},
		{
			file: "274_chat_pending_pool_v4.up.sql",
			fragments: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_pending_v4",
				"ON agent_task_queue (chat_session_id, priority DESC, created_at ASC, id ASC)",
				"chat_session_id IS NOT NULL",
				"status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred', 'waiting_runtime')",
			},
			forbidden: []string{"CREATE UNIQUE INDEX"},
		},
		{
			file:      "274_chat_pending_pool_v4.down.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_agent_task_queue_chat_pending_v4"},
		},
		{
			file:      "275_drop_chat_pending_v3.up.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_agent_task_queue_chat_pending_v3"},
		},
		{
			file: "275_drop_chat_pending_v3.down.sql",
			fragments: []string{
				"CREATE INDEX CONCURRENTLY idx_agent_task_queue_chat_pending_v3",
				"ON agent_task_queue (chat_session_id, created_at DESC)",
				"chat_session_id IS NOT NULL",
				"status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')",
			},
			forbidden: []string{"IF NOT EXISTS", "waiting_runtime", "priority DESC"},
		},
		{
			file: "276_waiting_runtime_workspace_index.up.sql",
			fragments: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_waiting_runtime_workspace",
				"ON agent_task_queue (placement_workspace_id, priority DESC, created_at ASC, id ASC)",
				"status = 'waiting_runtime'",
			},
		},
		{
			file:      "276_waiting_runtime_workspace_index.down.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_agent_task_queue_waiting_runtime_workspace"},
		},
		{
			file: "277_runtime_pool_occupancy_index.up.sql",
			fragments: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_runtime_capacity",
				"ON agent_task_queue (runtime_id)",
				"status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')",
			},
			forbidden: []string{"waiting_runtime"},
		},
		{
			file:      "277_runtime_pool_occupancy_index.down.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_agent_task_queue_runtime_capacity"},
		},
		{
			file: "278_runtime_pool_deferred_due_index.up.sql",
			fragments: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_pool_deferred_due",
				"ON agent_task_queue (placement_workspace_id, fire_at ASC, id ASC)",
				"runtime_binding_mode = 'pool'",
				"status = 'deferred'",
			},
		},
		{
			file:      "278_runtime_pool_deferred_due_index.down.sql",
			fragments: []string{"DROP INDEX CONCURRENTLY idx_agent_task_queue_pool_deferred_due"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			statements := sqlStatementsForLint(string(raw))
			if len(statements) != 1 {
				t.Fatalf("%s has %d top-level statements, want 1", tc.file, len(statements))
			}
			sql := normalizeSQL(statements[0])
			for _, fragment := range tc.fragments {
				if !strings.Contains(sql, normalizeSQL(fragment)) {
					t.Fatalf("%s missing %q\n%s", tc.file, fragment, sql)
				}
			}
			for _, fragment := range tc.forbidden {
				if strings.Contains(sql, normalizeSQL(fragment)) {
					t.Fatalf("%s contains forbidden %q\n%s", tc.file, fragment, sql)
				}
			}
		})
	}
}

func TestRuntimePoolRollbackGuardMigrationContract(t *testing.T) {
	dir := realMigrationsDir(t)
	up, err := os.ReadFile(filepath.Join(dir, "279_runtime_pool_rollback_guard.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(up) != "SELECT 1;\n" {
		t.Fatalf("279 up must be exactly SELECT 1; followed by newline, got %q", string(up))
	}

	down, err := os.ReadFile(filepath.Join(dir, "279_runtime_pool_rollback_guard.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	want := `DO $runtime_pool_rollback_guard$
BEGIN
    IF EXISTS (SELECT 1 FROM agent WHERE runtime_binding_mode = 'pool')
        OR EXISTS (SELECT 1 FROM agent_task_queue WHERE runtime_binding_mode = 'pool')
        OR EXISTS (SELECT 1 FROM platform_extension_release WHERE runtime_binding_mode = 'pool') THEN
        RAISE EXCEPTION 'runtime pool rows exist; rollback refused'
            USING ERRCODE = '23514';
    END IF;
END
$runtime_pool_rollback_guard$;
`
	if string(down) != want {
		t.Fatalf("279 down must be the single stable guard statement\ngot:\n%s\nwant:\n%s", down, want)
	}
}

func TestRuntimePoolIndexesAgainstDatabase(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expected := map[string]bool{
		"idx_one_pending_task_per_issue_agent_v3":        true,
		"idx_agent_task_queue_chat_pending_v4":           false,
		"idx_agent_task_queue_waiting_runtime_workspace": false,
		"idx_agent_task_queue_runtime_capacity":          false,
		"idx_agent_task_queue_pool_deferred_due":         false,
	}
	for indexName, wantUnique := range expected {
		t.Run(indexName+"/validity", func(t *testing.T) {
			var valid, unique bool
			if err := pool.QueryRow(ctx, `
				SELECT i.indisvalid, i.indisunique
				FROM pg_index AS i
				JOIN pg_class AS c ON c.oid = i.indexrelid
				JOIN pg_namespace AS n ON n.oid = c.relnamespace
				WHERE n.nspname = current_schema() AND c.relname = $1
			`, indexName).Scan(&valid, &unique); err != nil {
				t.Fatalf("read index %s: %v", indexName, err)
			}
			if !valid {
				t.Fatalf("index %s is INVALID", indexName)
			}
			if unique != wantUnique {
				t.Fatalf("index %s indisunique = %v, want %v", indexName, unique, wantUnique)
			}
		})
	}

	t.Run("issue_waiting_uniqueness_and_v2_compatibility", func(t *testing.T) {
		assertRuntimePoolIssueDuplicate(t, pool, "waiting_runtime")
		assertRuntimePoolIssueDuplicate(t, pool, "queued")
		assertRuntimePoolIssueDuplicate(t, pool, "channel_media_deferred")
		assertRuntimePoolIssueAllowsDifferentAgents(t, pool)
	})

	t.Run("chat_waiting_rows_remain_non_unique", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		fixture.ensurePoolAgent(t, pool)
		fixture.ensureChatSession(t, pool)
		sp, err := fixture.tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollbackSavepoint(t, sp)
		for i := 0; i < 2; i++ {
			if _, err := sp.Exec(ctx, `
				INSERT INTO agent_task_queue (
					agent_id, chat_session_id, status, priority, runtime_binding_mode,
					placement_workspace_id, runtime_requester_user_id, session_affinity_state
				) VALUES ($1, $2, 'waiting_runtime', $3, 'pool', $4, $5, 'none')
			`, fixture.poolAgentID, fixture.chatSessionID, i, fixture.workspaceID, fixture.userID); err != nil {
				t.Fatalf("insert Chat waiting row %d: %v", i+1, err)
			}
		}
	})
}

func TestRuntimePoolIndexPlannerEligibility(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSavepoint(t, tx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scans: %v", err)
	}
	// Competing partial indexes can filter these predicates on an empty test
	// table, but only the Pool indexes provide each query's exact ordering.
	// Disabling explicit Sort makes that boundary observable without changing
	// global statistics or relying on a large synthetic fixture.
	if _, err := tx.Exec(ctx, "SET LOCAL enable_sort = off"); err != nil {
		t.Fatalf("disable explicit sorts: %v", err)
	}

	var workspaceID, runtimeID, chatSessionID string
	if err := tx.QueryRow(ctx, "SELECT gen_random_uuid(), gen_random_uuid(), gen_random_uuid()").Scan(&workspaceID, &runtimeID, &chatSessionID); err != nil {
		t.Fatalf("generate planner fixture IDs: %v", err)
	}
	testCases := []struct {
		name      string
		indexName string
		query     string
		args      []any
	}{
		{
			name:      "workspace_waiting",
			indexName: "idx_agent_task_queue_waiting_runtime_workspace",
			query: `SELECT id FROM agent_task_queue
				WHERE placement_workspace_id = $1 AND status = 'waiting_runtime'
				ORDER BY priority DESC, created_at ASC, id ASC
				LIMIT 64 FOR UPDATE SKIP LOCKED`,
			args: []any{workspaceID},
		},
		{
			name:      "workspace_pool_deferred_due",
			indexName: "idx_agent_task_queue_pool_deferred_due",
			query: `SELECT id FROM agent_task_queue
				WHERE placement_workspace_id = $1
				  AND runtime_binding_mode = 'pool'
				  AND status = 'deferred'
				  AND fire_at <= now()
				ORDER BY fire_at ASC, id ASC
				LIMIT 64 FOR UPDATE SKIP LOCKED`,
			args: []any{workspaceID},
		},
		{
			name:      "runtime_capacity",
			indexName: "idx_agent_task_queue_runtime_capacity",
			query: `SELECT id FROM agent_task_queue
				WHERE runtime_id = $1
				  AND status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
				LIMIT 1 FOR UPDATE SKIP LOCKED`,
			args: []any{runtimeID},
		},
		{
			name:      "chat_six_status_pending",
			indexName: "idx_agent_task_queue_chat_pending_v4",
			query: `SELECT id FROM agent_task_queue
				WHERE chat_session_id = $1
				  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred', 'waiting_runtime')
				ORDER BY priority DESC, created_at ASC, id ASC
				LIMIT 64 FOR UPDATE SKIP LOCKED`,
			args: []any{chatSessionID},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			indexes := explainIndexNames(t, ctx, tx, tc.query, tc.args...)
			if !indexes[tc.indexName] {
				t.Fatalf("planner indexes = %v, want %s beneath the plan tree", sortedMapKeys(indexes), tc.indexName)
			}
		})
	}
}

func TestRuntimePoolRollbackGuardAgainstDatabase(t *testing.T) {
	pool := pooltestdb.Open(t)
	down, err := os.ReadFile(filepath.Join(realMigrationsDir(t), "279_runtime_pool_rollback_guard.down.sql"))
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name        string
		seed        func(*testing.T, *runtimePoolContractFixture)
		wantRefusal bool
	}{
		{name: "fixed_only"},
		{
			name: "pool_agent",
			seed: func(t *testing.T, fixture *runtimePoolContractFixture) {
				fixture.ensurePoolAgent(t, pool)
			},
			wantRefusal: true,
		},
		{
			name: "pool_task",
			seed: func(t *testing.T, fixture *runtimePoolContractFixture) {
				if _, err := fixture.tx.Exec(context.Background(), `
					INSERT INTO agent_task_queue (
						agent_id, issue_id, status, runtime_binding_mode,
						placement_workspace_id, runtime_requester_user_id, session_affinity_state
					) VALUES ($1, $2, 'waiting_runtime', 'pool', $3, $4, 'none')
				`, fixture.fixedAgentID, fixture.issueID, fixture.workspaceID, fixture.userID); err != nil {
					t.Fatalf("seed Pool task: %v", err)
				}
			},
			wantRefusal: true,
		},
		{
			name: "pool_release",
			seed: func(t *testing.T, fixture *runtimePoolContractFixture) {
				seed := time.Now().UnixNano()
				if _, err := fixture.tx.Exec(context.Background(), `
					INSERT INTO platform_extension_release (
						workspace_id, extension_key, name, version, digest, manifest,
						resources, created_by, runtime_binding_mode, runtime_requirements
					) VALUES ($1, $2, 'Pool Guard', $3, 'digest', '{}'::jsonb,
						'{}'::jsonb, $4, 'pool', '{}'::jsonb)
				`, fixture.workspaceID, fmt.Sprintf("pool.guard.%d", seed), fmt.Sprintf("1.0.%d", seed), fixture.userID); err != nil {
					t.Fatalf("seed Pool release: %v", err)
				}
			},
			wantRefusal: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRuntimePoolContractFixture(t, pool)
			if tc.seed != nil {
				tc.seed(t, fixture)
			}
			sp, err := fixture.tx.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, execErr := sp.Exec(context.Background(), string(down))
			rollbackSavepoint(t, sp)
			if !tc.wantRefusal {
				if execErr != nil {
					t.Fatalf("fixed-only rollback guard: %v", execErr)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(execErr, &pgErr) {
				t.Fatalf("guard error = %v, want PostgreSQL error", execErr)
			}
			if pgErr.Code != "23514" || pgErr.Message != "runtime pool rows exist; rollback refused" {
				t.Fatalf("guard error code/message = %s/%q", pgErr.Code, pgErr.Message)
			}
		})
	}
}

func assertRuntimePoolIssueDuplicate(t *testing.T, pool *pgxpool.Pool, status string) {
	t.Helper()
	fixture := newRuntimePoolContractFixture(t, pool)
	ctx := context.Background()

	var insertSQL string
	var args []any
	switch status {
	case "waiting_runtime":
		fixture.ensurePoolAgent(t, pool)
		insertSQL = `INSERT INTO agent_task_queue (
			agent_id, issue_id, status, runtime_binding_mode,
			placement_workspace_id, runtime_requester_user_id, session_affinity_state
		) VALUES ($1, $2, 'waiting_runtime', 'pool', $3, $4, 'none')`
		args = []any{fixture.poolAgentID, fixture.issueID, fixture.workspaceID, fixture.userID}
	case "queued":
		insertSQL = `INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id)
			VALUES ($1, $2, 'queued', $3)`
		args = []any{fixture.fixedAgentID, fixture.issueID, fixture.runtimeID}
	case "channel_media_deferred":
		insertSQL = `INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id, context)
			VALUES ($1, $2, 'deferred', $3, '{"channel_issue_media_pending":true}'::jsonb)`
		args = []any{fixture.fixedAgentID, fixture.issueID, fixture.runtimeID}
	default:
		t.Fatalf("unknown duplicate shape %q", status)
	}
	sp, err := fixture.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSavepoint(t, sp)
	if _, err := sp.Exec(ctx, insertSQL, args...); err != nil {
		t.Fatalf("insert first %s task: %v", status, err)
	}
	_, err = sp.Exec(ctx, insertSQL, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second %s task error = %v, want SQLSTATE 23505", status, err)
	}
	if pgErr.ConstraintName != "idx_one_pending_task_per_issue_agent_v3" {
		t.Fatalf("second %s task constraint = %q, want v3", status, pgErr.ConstraintName)
	}
}

func assertRuntimePoolIssueAllowsDifferentAgents(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	fixture := newRuntimePoolContractFixture(t, pool)
	fixture.ensurePoolAgent(t, pool)
	ctx := context.Background()
	var secondAgentID string
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args, runtime_binding_mode, runtime_requirements
		) VALUES ($1, $2, '', 'pool', '{}'::jsonb, NULL, 'private', 'private', 1, $3,
			'', '{}'::jsonb, '[]'::jsonb, 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}'::jsonb)
		RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Second Pool Agent %d", time.Now().UnixNano()), fixture.userID).Scan(&secondAgentID); err != nil {
		t.Fatalf("create second Pool Agent: %v", err)
	}
	sp, err := fixture.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSavepoint(t, sp)
	for _, agentID := range []string{fixture.poolAgentID, secondAgentID} {
		if _, err := sp.Exec(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, status, runtime_binding_mode,
				placement_workspace_id, runtime_requester_user_id, session_affinity_state
			) VALUES ($1, $2, 'waiting_runtime', 'pool', $3, $4, 'none')
		`, agentID, fixture.issueID, fixture.workspaceID, fixture.userID); err != nil {
			t.Fatalf("insert waiting task for distinct Agent %s: %v", agentID, err)
		}
	}
}

func explainIndexNames(t *testing.T, ctx context.Context, tx pgx.Tx, query string, args ...any) map[string]bool {
	t.Helper()
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain query: %v\n%s", err, query)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode EXPLAIN JSON: %v\n%s", err, raw)
	}
	indexes := make(map[string]bool)
	collectPlanIndexNames(plan, indexes)
	return indexes
}

func collectPlanIndexNames(value any, indexes map[string]bool) {
	switch node := value.(type) {
	case map[string]any:
		if name, ok := node["Index Name"].(string); ok {
			indexes[name] = true
		}
		for _, child := range node {
			collectPlanIndexNames(child, indexes)
		}
	case []any:
		for _, child := range node {
			collectPlanIndexNames(child, indexes)
		}
	}
}

func sortedMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func rollbackSavepoint(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("rollback test transaction/savepoint: %v", err)
	}
}

func normalizeSQL(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
