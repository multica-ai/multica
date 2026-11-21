package main

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGitHubPRAddressIndexMigrationUpDownUp(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	schema := createScratchSchema(t, ctx, adminPool, "migrate_github_pr_address_")
	pool := openTestPoolWithSearchPath(t, schema)
	// Keep the 3,000-row target repository outside the 100-value MCV lists while
	// preserving the owner/repository correlation that caused the planner to
	// underestimate this path and prefer it over the selective head_sha index.
	for _, statement := range []string{
		`CREATE TABLE github_pull_request (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			installation_id BIGINT NOT NULL,
			repo_owner TEXT NOT NULL,
			repo_name TEXT NOT NULL,
			pr_number INTEGER NOT NULL,
			head_sha TEXT NOT NULL,
			state TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key
			ON github_pull_request (workspace_id, repo_owner, repo_name, pr_number)`,
		`CREATE INDEX idx_github_pull_request_head_sha
			ON github_pull_request (head_sha)`,
		`INSERT INTO github_pull_request (
			id, workspace_id, installation_id, repo_owner, repo_name, pr_number, head_sha, state
		)
		SELECT md5('target-id-' || n)::uuid,
		       md5('target-workspace-' || n)::uuid,
		       777, 'target-owner', 'target-repo', n, 'target-sha-' || n, 'open'
		FROM generate_series(1, 3000) AS n
		UNION ALL
		SELECT md5('background-id-' || repo || '-' || n)::uuid,
		       md5('background-workspace-' || repo || '-' || n)::uuid,
		       1000 + repo, 'background-owner-' || repo, 'background-repo-' || repo,
		       n, 'background-sha-' || repo || '-' || n, 'open'
		FROM generate_series(1, 101) AS repo
		CROSS JOIN generate_series(1, 4000) AS n
		UNION ALL
		SELECT md5('singleton-id-' || n)::uuid,
		       md5('singleton-workspace-' || n)::uuid,
		       10000 + n, 'singleton-owner-' || n, 'singleton-repo-' || n,
		       1, 'singleton-sha-' || n, 'open'
		FROM generate_series(1, 3000) AS n`,
		`ALTER TABLE github_pull_request ALTER COLUMN installation_id SET STATISTICS 100`,
		`ALTER TABLE github_pull_request ALTER COLUMN repo_owner SET STATISTICS 100`,
		`ALTER TABLE github_pull_request ALTER COLUMN repo_name SET STATISTICS 100`,
		`ANALYZE github_pull_request`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply fixture statement: %v", err)
		}
	}

	const indexName = "idx_github_pull_request_pr_owner_repo"
	const addressQuery = `
		SELECT id, workspace_id, head_sha, state
		FROM github_pull_request
		WHERE installation_id = 777
		  AND repo_owner = 'target-owner'
		  AND repo_name = 'target-repo'
		  AND pr_number = 2999`
	const headSHAQuery = `
		SELECT DISTINCT pr_number
		FROM github_pull_request
		WHERE installation_id = 777
		  AND repo_owner = 'target-owner'
		  AND repo_name = 'target-repo'
		  AND head_sha = 'target-sha-2999'`
	addressBefore := explainAnalyze(t, ctx, pool, addressQuery)
	headSHABefore := explainAnalyze(t, ctx, pool, headSHAQuery)
	if strings.Contains(addressBefore, indexName) {
		t.Fatalf("before plan unexpectedly uses absent index: %s", addressBefore)
	}
	if !strings.Contains(headSHABefore, "idx_github_pull_request_head_sha") {
		t.Fatalf("before SHA plan does not use head_sha index: %s", headSHABefore)
	}

	const version = "535_github_pr_address_index"
	options := runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, []string{version}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply GitHub PR address index migration: %v", err)
	}
	assertGitHubPRAddressIndex(t, ctx, pool, indexName)
	addressAfter := explainAnalyze(t, ctx, pool, addressQuery)
	headSHAAfter := explainAnalyze(t, ctx, pool, headSHAQuery)
	if !strings.Contains(addressAfter, indexName) {
		t.Fatalf("after address plan does not use %s: %s", indexName, addressAfter)
	}
	if !strings.Contains(headSHAAfter, "idx_github_pull_request_head_sha") || strings.Contains(headSHAAfter, indexName) {
		t.Fatalf("after SHA plan does not stay on head_sha index: %s", headSHAAfter)
	}
	t.Logf(
		"address before:\n%s\naddress after:\n%s\nSHA before:\n%s\nSHA after:\n%s",
		addressBefore, addressAfter, headSHABefore, headSHAAfter,
	)

	options.Direction = "down"
	options.Files = realMigrationFiles(t, []string{version}, "down")
	options.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("roll back GitHub PR address index migration: %v", err)
	}
	assertIndexExists(t, pool, schema, indexName, false)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)

	options.Direction = "up"
	options.Files = realMigrationFiles(t, []string{version}, "up")
	options.Hooks = hooksForDirection("up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("reapply GitHub PR address index migration: %v", err)
	}
	assertGitHubPRAddressIndex(t, ctx, pool, indexName)
}

func assertGitHubPRAddressIndex(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	indexName string,
) {
	t.Helper()
	var definition string
	var unique, valid, ready, nonPartial bool
	var keyAttributes, totalAttributes int
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_indexdef(indexrelid), indisunique, indisvalid, indisready,
		       indpred IS NULL, indnkeyatts, indnatts
		FROM pg_index
		WHERE indexrelid = $1::regclass
	`, indexName).Scan(
		&definition, &unique, &valid, &ready, &nonPartial, &keyAttributes, &totalAttributes,
	); err != nil {
		t.Fatalf("read GitHub PR address index: %v", err)
	}
	if unique || !valid || !ready || !nonPartial || keyAttributes != 3 || totalAttributes != 3 {
		t.Fatalf(
			"index flags unique=%v valid=%v ready=%v non-partial=%v keys=%d attributes=%d",
			unique, valid, ready, nonPartial, keyAttributes, totalAttributes,
		)
	}
	if !strings.Contains(definition, "USING btree (pr_number, repo_owner, repo_name)") {
		t.Fatalf("index definition = %q", definition)
	}
}

func explainAnalyze(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) string {
	t.Helper()
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query)
	if err != nil {
		t.Fatalf("explain query: %v", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read explain rows: %v", err)
	}
	return strings.Join(lines, "\n")
}
