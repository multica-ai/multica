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

const dingtalkConnectorMigrationTestSchema = "dingtalk_connector_migration_test"
const channelInboundAuditWorkspaceMigrationTestSchema = "channel_inbound_audit_workspace_migration_test"

var dingtalkConnectorUpMigrations = []string{
	"342_dingtalk_connector.up.sql",
	"343_dingtalk_workspace_grant.up.sql",
	"344_dingtalk_direct_route.up.sql",
	"345_dingtalk_connector_cutover.up.sql",
	"346_dingtalk_connector_id_unique.up.sql",
	"347_dingtalk_connector_app_id_unique.up.sql",
	"348_dingtalk_connector_lease_index.up.sql",
	"349_dingtalk_workspace_grant_id_unique.up.sql",
	"350_dingtalk_workspace_grant_connector_workspace_unique.up.sql",
	"351_dingtalk_workspace_grant_workspace_index.up.sql",
	"352_dingtalk_direct_route_id_unique.up.sql",
	"353_dingtalk_direct_route_connector_user_unique.up.sql",
	"354_dingtalk_direct_route_workspace_index.up.sql",
	"355_dingtalk_connector_rollback_guard.up.sql",
	"358_dingtalk_connector_rollback_guard.up.sql",
}

var dingtalkConnectorDownMigrations = []string{
	"358_dingtalk_connector_rollback_guard.down.sql",
	"355_dingtalk_connector_rollback_guard.down.sql",
	"354_dingtalk_direct_route_workspace_index.down.sql",
	"353_dingtalk_direct_route_connector_user_unique.down.sql",
	"352_dingtalk_direct_route_id_unique.down.sql",
	"351_dingtalk_workspace_grant_workspace_index.down.sql",
	"350_dingtalk_workspace_grant_connector_workspace_unique.down.sql",
	"349_dingtalk_workspace_grant_id_unique.down.sql",
	"348_dingtalk_connector_lease_index.down.sql",
	"347_dingtalk_connector_app_id_unique.down.sql",
	"346_dingtalk_connector_id_unique.down.sql",
	"345_dingtalk_connector_cutover.down.sql",
	"344_dingtalk_direct_route.down.sql",
	"343_dingtalk_workspace_grant.down.sql",
	"342_dingtalk_connector.down.sql",
}

func TestDingTalkConnectorMigrationFilesExist(t *testing.T) {
	for _, name := range append(dingtalkConnectorUpMigrations, dingtalkConnectorDownMigrations...) {
		_ = readMigrationFile(t, name)
	}
}

func TestDingTalkConnectorMigrationsUpDownAndBackfill(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+dingtalkConnectorMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+dingtalkConnectorMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, dingtalkConnectorMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	createLegacyDingTalkSchema(t, ctx, conn.Conn())
	seedLegacyDingTalkInstallation(t, ctx, conn.Conn())

	for _, name := range dingtalkConnectorUpMigrations {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}
	// The runner records schema_migrations after executing each SQL file. Both
	// cutover directions must be safe to retry if that bookkeeping write fails.
	applyMigrationFile(t, ctx, conn.Conn(), "345_dingtalk_connector_cutover.up.sql")

	assertDingTalkConnectorBackfill(t, ctx, conn.Conn())
	assertLegacyDingTalkWritesParked(t, ctx, conn.Conn())
	assertDingTalkConnectorIndexes(t, ctx, conn.Conn())
	assertDingTalkRollbackGuardRunsBeforeIndexDrops(t, ctx, conn.Conn())
	assertDingTalkRollbackWritesParked(t, ctx, conn.Conn())

	for _, name := range dingtalkConnectorDownMigrations {
		applyMigrationFile(t, ctx, conn.Conn(), name)
		if name == "345_dingtalk_connector_cutover.down.sql" {
			assertDingTalkConnectorParkedForRollback(t, ctx, conn.Conn())
		}
	}

	var restoredWorkspaceID, restoredAgentID string
	if err := conn.QueryRow(ctx, `
		SELECT workspace_id::text, agent_id::text
		FROM channel_installation
		WHERE id = 'd2750000-0000-4000-8000-000000000001'
	`).Scan(&restoredWorkspaceID, &restoredAgentID); err != nil {
		t.Fatalf("inspect restored legacy installation: %v", err)
	}
	if restoredWorkspaceID != "d2750000-0000-4000-8000-000000000002" || restoredAgentID != "d2750000-0000-4000-8000-000000000003" {
		t.Fatalf("restored installation target = (%s, %s), want original workspace and agent", restoredWorkspaceID, restoredAgentID)
	}
}

func TestChannelInboundAuditWorkspaceMigrationBackfillAndIndex(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+channelInboundAuditWorkspaceMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+channelInboundAuditWorkspaceMigrationTestSchema); err != nil {
		t.Fatalf("create isolated audit migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, channelInboundAuditWorkspaceMigrationTestSchema); err != nil {
		t.Fatalf("set audit migration search path: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		CREATE TABLE dingtalk_workspace_grant (
			connector_id UUID NOT NULL,
			workspace_id UUID NOT NULL
		);
		CREATE TABLE channel_inbound_audit (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			installation_id UUID,
			channel_type TEXT NOT NULL,
			received_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO dingtalk_workspace_grant (connector_id, workspace_id) VALUES
			('d2760000-0000-4000-8000-000000000001', 'd2760000-0000-4000-8000-000000000002'),
			('d2760000-0000-4000-8000-000000000003', 'd2760000-0000-4000-8000-000000000004'),
			('d2760000-0000-4000-8000-000000000003', 'd2760000-0000-4000-8000-000000000005');
		INSERT INTO channel_inbound_audit (installation_id, channel_type) VALUES
			('d2760000-0000-4000-8000-000000000001', 'dingtalk'),
			('d2760000-0000-4000-8000-000000000003', 'dingtalk');
	`); err != nil {
		t.Fatalf("seed audit migration fixture: %v", err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "356_channel_inbound_audit_workspace.up.sql")
	var singleWorkspace, ambiguousWorkspace *string
	if err := conn.QueryRow(ctx, `
		SELECT max(workspace_id::text) FILTER (WHERE installation_id = 'd2760000-0000-4000-8000-000000000001'),
		       max(workspace_id::text) FILTER (WHERE installation_id = 'd2760000-0000-4000-8000-000000000003')
		FROM channel_inbound_audit
	`).Scan(&singleWorkspace, &ambiguousWorkspace); err != nil {
		t.Fatalf("inspect audit workspace backfill: %v", err)
	}
	if singleWorkspace == nil || *singleWorkspace != "d2760000-0000-4000-8000-000000000002" || ambiguousWorkspace != nil {
		t.Fatalf("audit backfill = single %v ambiguous %v", singleWorkspace, ambiguousWorkspace)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "357_channel_inbound_audit_workspace_index.up.sql")
	var valid bool
	if err := conn.QueryRow(ctx, `
		SELECT i.indisvalid
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = idx.relnamespace
		WHERE n.nspname = $1 AND idx.relname = 'idx_channel_inbound_audit_workspace'
	`, channelInboundAuditWorkspaceMigrationTestSchema).Scan(&valid); err != nil || !valid {
		t.Fatalf("audit workspace index valid = %t, err=%v", valid, err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "357_channel_inbound_audit_workspace_index.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "356_channel_inbound_audit_workspace.down.sql")
}

func assertDingTalkRollbackGuardRunsBeforeIndexDrops(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_workspace_grant (
			id, connector_id, workspace_id, default_agent_id, installer_user_id
		) VALUES (
			'd2750000-0000-4000-8000-000000000010',
			'd2750000-0000-4000-8000-000000000001',
			'd2750000-0000-4000-8000-000000000011',
			'd2750000-0000-4000-8000-000000000012',
			'd2750000-0000-4000-8000-000000000004'
		)
	`); err != nil {
		t.Fatalf("seed second rollback grant: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigrationFile(t, "358_dingtalk_connector_rollback_guard.down.sql")); err == nil {
		t.Fatal("rollback guard allowed a multi-workspace connector")
	}
	assertDingTalkConnectorIndex(t, ctx, conn, "idx_dingtalk_direct_route_workspace", false)
	if _, err := conn.Exec(ctx, `DELETE FROM dingtalk_workspace_grant WHERE id = 'd2750000-0000-4000-8000-000000000010'`); err != nil {
		t.Fatalf("remove second rollback grant: %v", err)
	}
}

func createLegacyDingTalkSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `
		CREATE TABLE channel_installation (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			agent_id UUID NOT NULL,
			channel_type TEXT NOT NULL,
			config JSONB NOT NULL DEFAULT '{}'::jsonb,
			status TEXT NOT NULL DEFAULT 'active',
			ws_lease_token TEXT,
			ws_lease_expires_at TIMESTAMPTZ,
			installer_user_id UUID NOT NULL,
			installed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (workspace_id, agent_id, channel_type)
		);
		CREATE UNIQUE INDEX idx_channel_installation_type_appid
			ON channel_installation(channel_type, (config ->> 'app_id'));
		CREATE TABLE dingtalk_group_route (
			id UUID NOT NULL DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			installation_id UUID NOT NULL,
			conversation_id TEXT NOT NULL,
			agent_id UUID NOT NULL,
			revision BIGINT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		t.Fatalf("create legacy DingTalk schema: %v", err)
	}
}

func seedLegacyDingTalkInstallation(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config, status,
			ws_lease_token, ws_lease_expires_at, installer_user_id
		) VALUES (
			'd2750000-0000-4000-8000-000000000001',
			'd2750000-0000-4000-8000-000000000002',
			'd2750000-0000-4000-8000-000000000003',
			'dingtalk',
			'{"app_id":"ding-app","app_secret_encrypted":"c2VhbGVk"}'::jsonb,
			'active', 'lease-token', now() + interval '5 minutes',
			'd2750000-0000-4000-8000-000000000004'
		), (
			'd2750000-0000-4000-8000-000000000005',
			'd2750000-0000-4000-8000-000000000002',
			'd2750000-0000-4000-8000-000000000006',
			'feishu', '{"app_id":"feishu-app"}'::jsonb, 'active',
			NULL, NULL, 'd2750000-0000-4000-8000-000000000004'
		);
		INSERT INTO dingtalk_group_route (
			id, workspace_id, installation_id, conversation_id, agent_id
		) VALUES (
			'd2750000-0000-4000-8000-000000000007',
			'd2750000-0000-4000-8000-000000000002',
			'd2750000-0000-4000-8000-000000000001',
			'group-a',
			'd2750000-0000-4000-8000-000000000003'
		)
	`); err != nil {
		t.Fatalf("seed legacy DingTalk installation: %v", err)
	}
}

func assertDingTalkConnectorBackfill(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var appID, status, leaseToken string
	if err := conn.QueryRow(ctx, `
		SELECT app_id, status, ws_lease_token
		FROM dingtalk_connector
		WHERE id = 'd2750000-0000-4000-8000-000000000001'
	`).Scan(&appID, &status, &leaseToken); err != nil {
		t.Fatalf("inspect migrated connector: %v", err)
	}
	if appID != "ding-app" || status != "active" || leaseToken != "lease-token" {
		t.Fatalf("migrated connector = (%s, %s, %s), want preserved app, status, and lease", appID, status, leaseToken)
	}

	var workspaceID, agentID string
	if err := conn.QueryRow(ctx, `
		SELECT workspace_id::text, default_agent_id::text
		FROM dingtalk_workspace_grant
		WHERE connector_id = 'd2750000-0000-4000-8000-000000000001'
	`).Scan(&workspaceID, &agentID); err != nil {
		t.Fatalf("inspect migrated workspace grant: %v", err)
	}
	if workspaceID != "d2750000-0000-4000-8000-000000000002" || agentID != "d2750000-0000-4000-8000-000000000003" {
		t.Fatalf("migrated grant = (%s, %s), want original workspace and agent", workspaceID, agentID)
	}

	var legacyDingTalkCount, legacyFeishuCount, routeCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE channel_type = 'dingtalk'`).Scan(&legacyDingTalkCount); err != nil {
		t.Fatalf("count legacy DingTalk installations: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE channel_type = 'feishu'`).Scan(&legacyFeishuCount); err != nil {
		t.Fatalf("count legacy Feishu installations: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FROM dingtalk_group_route
		WHERE installation_id = 'd2750000-0000-4000-8000-000000000001'
	`).Scan(&routeCount); err != nil {
		t.Fatalf("count preserved group routes: %v", err)
	}
	if legacyDingTalkCount != 0 || legacyFeishuCount != 1 || routeCount != 1 {
		t.Fatalf("cutover counts = dingtalk %d, feishu %d, route %d; want 0, 1, 1", legacyDingTalkCount, legacyFeishuCount, routeCount)
	}
}

func assertLegacyDingTalkWritesParked(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	_, err := conn.Exec(ctx, `
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config, installer_user_id
		) VALUES (
			'd2750000-0000-4000-8000-000000000020',
			'd2750000-0000-4000-8000-000000000002',
			'd2750000-0000-4000-8000-000000000021',
			'dingtalk', '{"app_id":"late-ding-app"}'::jsonb,
			'd2750000-0000-4000-8000-000000000004'
		)
	`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("legacy DingTalk write error = %v, want SQLSTATE 55000", err)
	}

	if _, err := conn.Exec(ctx, `
		UPDATE channel_installation
		SET config = config || '{"cutover_probe":true}'::jsonb
		WHERE id = 'd2750000-0000-4000-8000-000000000005'
	`); err != nil {
		t.Fatalf("non-DingTalk write blocked by cutover gate: %v", err)
	}
}

func assertDingTalkConnectorIndexes(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for name, wantUnique := range map[string]bool{
		"idx_dingtalk_connector_id_unique":                        true,
		"idx_dingtalk_connector_app_id_unique":                    true,
		"idx_dingtalk_connector_lease":                            false,
		"idx_dingtalk_workspace_grant_id_unique":                  true,
		"idx_dingtalk_workspace_grant_connector_workspace_unique": true,
		"idx_dingtalk_workspace_grant_workspace":                  false,
		"idx_dingtalk_direct_route_id_unique":                     true,
		"idx_dingtalk_direct_route_connector_user_unique":         true,
		"idx_dingtalk_direct_route_workspace":                     false,
	} {
		assertDingTalkConnectorIndex(t, ctx, conn, name, wantUnique)
	}

	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_connector (
			id, app_id, config, installer_user_id
		) VALUES (
			'd2750000-0000-4000-8000-000000000008', 'ding-app', '{}',
			'd2750000-0000-4000-8000-000000000004'
		)
	`); !isUniqueViolationMigration(err) {
		t.Fatalf("duplicate connector app id error = %v, want unique violation", err)
	}

	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_workspace_grant (
			id, connector_id, workspace_id, default_agent_id, installer_user_id
		) VALUES (
			'd2750000-0000-4000-8000-000000000009',
			'd2750000-0000-4000-8000-000000000001',
			'd2750000-0000-4000-8000-000000000002',
			'd2750000-0000-4000-8000-000000000003',
			'd2750000-0000-4000-8000-000000000004'
		)
	`); !isUniqueViolationMigration(err) {
		t.Fatalf("duplicate connector workspace grant error = %v, want unique violation", err)
	}
}

func assertDingTalkRollbackWritesParked(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	applyMigrationFile(t, ctx, conn, "358_dingtalk_connector_rollback_guard.down.sql")
	_, err := conn.Exec(ctx, `
		UPDATE dingtalk_connector
		SET status = 'revoked'
		WHERE id = 'd2750000-0000-4000-8000-000000000001'
	`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("connector write during rollback error = %v, want SQLSTATE 55000", err)
	}
}

func assertDingTalkConnectorParkedForRollback(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	applyMigrationFile(t, ctx, conn, "345_dingtalk_connector_cutover.down.sql")
	var connectorStatus, legacyStatus string
	if err := conn.QueryRow(ctx, `
		SELECT status
		FROM dingtalk_connector
		WHERE id = 'd2750000-0000-4000-8000-000000000001'
	`).Scan(&connectorStatus); err != nil {
		t.Fatalf("inspect parked connector: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT status
		FROM channel_installation
		WHERE id = 'd2750000-0000-4000-8000-000000000001'
	`).Scan(&legacyStatus); err != nil {
		t.Fatalf("inspect restored legacy status: %v", err)
	}
	if connectorStatus != "revoked" || legacyStatus != "active" {
		t.Fatalf("rollback statuses = connector %q legacy %q, want revoked/active", connectorStatus, legacyStatus)
	}

	_, err := conn.Exec(ctx, `
		UPDATE dingtalk_connector
		SET status = 'active'
		WHERE id = 'd2750000-0000-4000-8000-000000000001'
	`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("connector unpark during rollback error = %v, want SQLSTATE 55000", err)
	}
}

func assertDingTalkConnectorIndex(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, wantUnique bool) {
	t.Helper()
	var unique bool
	err := conn.QueryRow(ctx, `
		SELECT i.indisunique
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = idx.relnamespace
		WHERE n.nspname = $1 AND idx.relname = $2
	`, dingtalkConnectorMigrationTestSchema, name).Scan(&unique)
	if err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	if unique != wantUnique {
		t.Fatalf("index %s unique = %t, want %t", name, unique, wantUnique)
	}
}
