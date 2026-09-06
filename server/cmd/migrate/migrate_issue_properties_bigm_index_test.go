package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// issuePropertiesBigramMigrations is the pair that has to move together: 446
// builds the expression index, 447 generates the statistics the planner needs
// before it will consider it. Applying 446 alone leaves the index built, valid
// and ignored.
var issuePropertiesBigramMigrations = []string{
	"446_issue_properties_bigm_index",
	"447_issue_properties_bigm_index_statistics",
}

// TestIssuePropertiesBigramIndexBuildsOnlyWherePGBigmExists runs migrations 446
// and 447's real SQL through the runner. The gate has to hold in both
// directions: the index appears where pg_bigm is installed, and where it is
// not — core Postgres, and the pgvector image CI and self-hosted deployments
// run — the version records with its SQL skipped rather than failing the run. A
// failure there would take backend startup with it, for an index that is only
// an optimization: the contains prefilter it serves stays correct
// unaccelerated.
//
// 447 is not gated, so its statistics refresh must land in both environments.
func TestIssuePropertiesBigramIndexBuildsOnlyWherePGBigmExists(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pgBigmUsable := installExtensionIfAvailable(t, ctx, adminPool, "pg_bigm")

	schema := createScratchSchema(t, ctx, adminPool, "migrate_issue_properties_bigm_")
	pool := openTestPoolWithSearchPath(t, schema+", public")
	seedIssuePropertiesFixture(t, ctx, pool, 500)

	// ANALYZE only records statistics for a table it has seen rows in, so the
	// post-migration assertions below are only meaningful against this
	// pre-state: nothing has analyzed the fixture yet.
	if got := statisticsRowCount(t, ctx, pool, schema, "issue"); got != 0 {
		t.Fatalf("fixture already has %d statistics rows before migrating", got)
	}

	migrationsTable := schema + ".schema_migrations"
	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, issuePropertiesBigramMigrations, "up"),
		SchemaMigrationsTable: migrationsTable,
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}); err != nil {
		t.Fatalf("apply properties bigram index: %v", err)
	}

	// Recorded either way: a skipped migration still advances the ledger, or
	// every later version would be blocked on a database without pg_bigm.
	for _, version := range issuePropertiesBigramMigrations {
		assertMigrationVersionRecorded(t, ctx, pool, schema, version, true)
	}
	assertIndexExists(t, pool, schema, "idx_issue_properties_bigm", pgBigmUsable)
	if pgBigmUsable {
		assertIndexValidity(t, pool, schema, "idx_issue_properties_bigm", true)
	}

	// 447 ran unconditionally, so the table's own column statistics exist in
	// every environment.
	if got := statisticsRowCount(t, ctx, pool, schema, "issue"); got == 0 {
		t.Fatal("447 did not analyze the table: no statistics rows")
	}
	// Where the index exists, the statistics that matter are the ones for its
	// expression — a separate pg_statistic entry keyed by the index relation,
	// and the only thing that lets the planner cost a contains prefilter.
	if pgBigmUsable {
		if got := statisticsRowCount(t, ctx, pool, schema, "idx_issue_properties_bigm"); got == 0 {
			t.Fatal("no statistics for the LOWER(properties::text) expression: the planner cannot cost the index")
		}
	}

	// The rollback drops unconditionally: it must be a no-op, not an error,
	// where the up direction never built anything. 447's rollback is a
	// comment-only no-op and must still execute cleanly.
	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "down",
		Files:                 realMigrationFiles(t, reversed(issuePropertiesBigramMigrations), "down"),
		SchemaMigrationsTable: migrationsTable,
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("down"),
	}); err != nil {
		t.Fatalf("roll back properties bigram index: %v", err)
	}
	for _, version := range issuePropertiesBigramMigrations {
		assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
	}
	assertIndexExists(t, pool, schema, "idx_issue_properties_bigm", false)
}

// TestIssuePropertiesBigramIndexNeedsAnalyzeForItsExpression is the regression
// 447 exists for, and it can only be written against the real extension.
//
// Building an expression index does not collect statistics for its expression.
// Until they exist the planner falls back to a pattern-length heuristic — a
// single-character LIKE is estimated at a few percent of the table regardless
// of what the data holds — so it cannot tell a needle matching 40 rows from one
// matching 40,000. On a large table that estimate loses to a sequential scan
// and the index it just built goes unused, which is the exact query class
// MUL-6928 added it for and the shape a CJK contains filter has. The failure is
// invisible by every other signal: the index is present, valid and ready.
//
// The assertion is on the estimate rather than the chosen plan because the
// estimate is the thing 447 repairs. Which plan that estimate wins is a
// function of table size: at fixture scale a wrong estimate still picks the
// index, and it takes a production-sized table for the same error to flip to a
// sequential scan.
func TestIssuePropertiesBigramIndexNeedsAnalyzeForItsExpression(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	if !installExtensionIfAvailable(t, ctx, adminPool, "pg_bigm") {
		t.Skip("Postgres does not provide pg_bigm; the planner behaviour under test is extension-specific")
	}

	schema := createScratchSchema(t, ctx, adminPool, "migrate_issue_properties_analyze_")
	pool := openTestPoolWithSearchPath(t, schema+", public")
	seedIssuePropertiesFixture(t, ctx, pool, 40000)

	// The prefilter clause on its own: the qual whose selectivity the missing
	// statistics get wrong.
	const prefilterQuery = `SELECT i.id FROM issue i WHERE LOWER(i.properties::text) LIKE LOWER('%鬻%')`
	var actual int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue i WHERE LOWER(i.properties::text) LIKE LOWER('%鬻%')`).Scan(&actual); err != nil {
		t.Fatalf("count rare needle matches: %v", err)
	}
	if actual == 0 {
		t.Fatal("fixture seeded no rows for the rare needle")
	}

	migrationsTable := schema + ".schema_migrations"
	applyMigration := func(t *testing.T, version string) {
		t.Helper()
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             "up",
			Files:                 realMigrationFiles(t, []string{version}, "up"),
			SchemaMigrationsTable: migrationsTable,
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection("up"),
			Conditions:            conditionsForDirection("up"),
		}); err != nil {
			t.Fatalf("apply %s: %v", version, err)
		}
	}

	applyMigration(t, "446_issue_properties_bigm_index")
	assertIndexValidity(t, pool, schema, "idx_issue_properties_bigm", true)
	withoutStatistics := estimatedRows(t, ctx, pool, prefilterQuery)
	if withoutStatistics < actual*10 {
		t.Fatalf("expected the index build alone to leave a badly wrong estimate, got %d for %d actual rows; "+
			"if Postgres now collects expression statistics at build time, 447 and this test can go",
			withoutStatistics, actual)
	}

	applyMigration(t, "447_issue_properties_bigm_index_statistics")
	if got := statisticsRowCount(t, ctx, pool, schema, "idx_issue_properties_bigm"); got == 0 {
		t.Fatal("447 generated no statistics for the LOWER(properties::text) expression")
	}
	withStatistics := estimatedRows(t, ctx, pool, prefilterQuery)
	if withStatistics*10 > withoutStatistics {
		t.Fatalf("447 did not improve the selectivity estimate: %d before, %d after, %d actual",
			withoutStatistics, withStatistics, actual)
	}
}

// seedIssuePropertiesFixture builds a scratch `issue` table shaped like the
// real one for the properties filter: a jsonb bag whose text value is drawn
// from a small shared vocabulary, plus a rare character on a few rows so a
// selective needle exists.
func seedIssuePropertiesFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE issue (
		id BIGSERIAL PRIMARY KEY,
		properties JSONB NOT NULL DEFAULT '{}'::jsonb
	)`); err != nil {
		t.Fatalf("create issue fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue (properties)
		SELECT jsonb_build_object(
			'text', (ARRAY['登录问题','支付延迟','看板导出','权限迁移'])[1 + (n % 4)]
				|| ' ' || (ARRAY['login','payment','dashboard','export'])[1 + (n % 4)]
				|| ' REF' || lpad(n::text, 7, '0')
				|| CASE WHEN n % 1000 = 0 THEN ' 鬻' ELSE '' END,
			'number', (n % 500)
		)
		FROM generate_series(1, $1) AS n
	`, rows); err != nil {
		t.Fatalf("seed issue fixture: %v", err)
	}
}

// statisticsRowCount reports how many pg_statistic rows exist for a relation.
// Expression-index statistics are keyed by the index relation, not the table,
// which is why the caller passes the relation it cares about.
func statisticsRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, relation string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_statistic s
		JOIN pg_class c ON c.oid = s.starelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`, schema, relation).Scan(&count); err != nil {
		t.Fatalf("read statistics for %s.%s: %v", schema, relation, err)
	}
	return count
}

// estimatedRows returns the planner's estimated row count for query — the
// top-level node's estimate, which for a bare filtering SELECT is how many rows
// it thinks the qual will keep.
func estimatedRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) int {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+query).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plans []struct {
		Plan struct {
			Rows int `json:"Plan Rows"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatalf("decode plan %s: %v", raw, err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %d", len(plans))
	}
	return plans[0].Plan.Rows
}

func reversed(versions []string) []string {
	out := make([]string, len(versions))
	for i, v := range versions {
		out[len(versions)-1-i] = v
	}
	return out
}

// TestOperatorClassAvailabilityFailsClosed checks the gate itself against real
// catalog rows. Every environment has pg_trgm (migration 137), so it stands in
// for "installed"; a condition that answered false for it would silently skip
// every gated migration forever, and one that answered true for an absent
// opclass would abort the run it exists to protect.
func TestOperatorClassAvailabilityFailsClosed(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := adminPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		t.Fatalf("install pg_trgm test dependency: %v", err)
	}
	pgBigmUsable := installExtensionIfAvailable(t, ctx, adminPool, "pg_bigm")

	conn, err := adminPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	for _, tc := range []struct {
		name    string
		opclass extensionOperatorClass
		want    bool
	}{
		{"installed", extensionOperatorClass{"gin", "gin_trgm_ops", "pg_trgm"}, true},
		{"wrong owning extension", extensionOperatorClass{"gin", "gin_trgm_ops", "pg_bigm"}, false},
		{"wrong access method", extensionOperatorClass{"btree", "gin_trgm_ops", "pg_trgm"}, false},
		{"unknown opclass", extensionOperatorClass{"gin", "gin_nonexistent_ops", "pg_trgm"}, false},
		{"migration 446 gate", issuePropertiesBigramOperatorClass, pgBigmUsable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apply, reason, err := whenOperatorClassAvailable(tc.opclass)(ctx, conn)
			if err != nil {
				t.Fatalf("evaluate condition: %v", err)
			}
			if apply != tc.want {
				t.Fatalf("apply=%v (%s), want %v", apply, reason, tc.want)
			}
			if !apply && reason == "" {
				t.Fatal("a skipped migration must report why")
			}
		})
	}
}

// installExtensionIfAvailable installs name when the server ships it and
// reports whether it is usable afterwards, so a test can assert the real
// behaviour of both environments instead of skipping on one of them.
func installExtensionIfAvailable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var available bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = $1)
	`, name).Scan(&available); err != nil {
		t.Fatalf("inspect %s availability: %v", name, err)
	}
	if !available {
		t.Logf("%s is not provided by this Postgres; asserting the skipped path", name)
		return false
	}
	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("install %s test dependency: %v", name, err)
	}
	return true
}

// createScratchSchema gives a migration test its own namespace so the real
// migrations can run against fixture tables without touching the shared
// database the rest of the suite uses.
func createScratchSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, prefix string) string {
	t.Helper()
	schema := fmt.Sprintf("%s%d_%d", prefix, time.Now().UnixNano(), rand.Uint32())
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})
	return schema
}
