package migrations

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dingtalkRouteMigrationTestSchema = "dingtalk_route_migration_test"

func TestDingTalkGroupRouteMigrationsUpDownAndCatalog(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+dingtalkRouteMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+dingtalkRouteMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, dingtalkRouteMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	for _, name := range []string{
		"304_dingtalk_group_route.up.sql",
		"305_dingtalk_group_route_installation_conversation_unique.up.sql",
		"306_dingtalk_group_route_workspace_index.up.sql",
		"307_dingtalk_group_route_id_unique.up.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}

	assertDingTalkRouteIndex(t, ctx, conn.Conn(), "idx_dingtalk_group_route_installation_conversation", true)
	assertDingTalkRouteIndex(t, ctx, conn.Conn(), "idx_dingtalk_group_route_workspace", false)
	assertDingTalkRouteIndex(t, ctx, conn.Conn(), "idx_dingtalk_group_route_id_unique", true)

	const stableID = "d2750000-0000-4000-8000-000000000001"
	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_group_route (
			id, workspace_id, installation_id, conversation_id, agent_id
		) VALUES
			($1, 'd2750000-0000-4000-8000-000000000002', 'd2750000-0000-4000-8000-000000000003', 'group-a', 'd2750000-0000-4000-8000-000000000004')
	`, stableID); err != nil {
		t.Fatalf("insert first stable route id: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_group_route (
			id, workspace_id, installation_id, conversation_id, agent_id
		) VALUES
			($1, 'd2750000-0000-4000-8000-000000000002', 'd2750000-0000-4000-8000-000000000005', 'group-b', 'd2750000-0000-4000-8000-000000000004')
	`, stableID); !isUniqueViolationMigration(err) {
		t.Fatalf("duplicate route id error = %v, want unique violation", err)
	}

	for _, name := range []string{
		"307_dingtalk_group_route_id_unique.down.sql",
		"306_dingtalk_group_route_workspace_index.down.sql",
		"305_dingtalk_group_route_installation_conversation_unique.down.sql",
		"304_dingtalk_group_route.down.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}

	var tableExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRouteMigrationTestSchema+".dingtalk_group_route").Scan(&tableExists); err != nil {
		t.Fatalf("inspect rolled-back route table: %v", err)
	}
	if tableExists {
		t.Fatal("dingtalk_group_route still exists after down migrations")
	}
}

func assertDingTalkRouteIndex(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, wantUnique bool) {
	t.Helper()
	var unique bool
	err := conn.QueryRow(ctx, `
		SELECT i.indisunique
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = idx.relnamespace
		WHERE n.nspname = $1 AND idx.relname = $2
	`, dingtalkRouteMigrationTestSchema, name).Scan(&unique)
	if err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	if unique != wantUnique {
		t.Fatalf("index %s unique = %t, want %t", name, unique, wantUnique)
	}
}

func isUniqueViolationMigration(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
