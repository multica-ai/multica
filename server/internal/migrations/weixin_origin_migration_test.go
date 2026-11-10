package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const weixinOriginMigrationTestSchema = "weixin_origin_migration_test"

func TestWeixinOriginMigrationsUpDownAndCatalog(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+weixinOriginMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+weixinOriginMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, weixinOriginMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue (
			id UUID PRIMARY KEY,
			origin_type TEXT NULL
		);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat'));
	`); err != nil {
		t.Fatalf("create pre-Weixin issue table: %v", err)
	}

	assertWeixinOriginRejected(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000001")

	applyMigrationFile(t, ctx, conn.Conn(), "645_issue_origin_weixin_chat.up.sql")
	assertWeixinOriginConstraint(t, ctx, conn.Conn(), false, true)
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, "00000000-0000-4000-8000-000000000002"); err != nil {
		t.Fatalf("insert weixin_chat after widening constraint: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "646_issue_origin_weixin_chat_validate.up.sql")
	assertWeixinOriginConstraint(t, ctx, conn.Conn(), true, true)

	applyMigrationFile(t, ctx, conn.Conn(), "646_issue_origin_weixin_chat_validate.down.sql")
	assertWeixinOriginConstraint(t, ctx, conn.Conn(), false, true)
	if _, err := conn.Exec(ctx, `DELETE FROM issue WHERE origin_type = 'weixin_chat'`); err != nil {
		t.Fatalf("remove Weixin row before narrowing rollback: %v", err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "645_issue_origin_weixin_chat.down.sql")
	assertWeixinOriginConstraint(t, ctx, conn.Conn(), true, false)
	assertWeixinOriginRejected(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000003")
}

func assertWeixinOriginConstraint(t *testing.T, ctx context.Context, conn *pgx.Conn, wantValidated, wantWeixin bool) {
	t.Helper()
	var validated bool
	var definition string
	if err := conn.QueryRow(ctx, `
		SELECT convalidated, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'issue'::regclass AND conname = 'issue_origin_type_check'
	`).Scan(&validated, &definition); err != nil {
		t.Fatalf("inspect issue origin constraint: %v", err)
	}
	if validated != wantValidated {
		t.Fatalf("constraint validated = %t, want %t", validated, wantValidated)
	}
	if strings.Contains(definition, "weixin_chat") != wantWeixin {
		t.Fatalf("constraint Weixin membership = %t, want %t: %s", strings.Contains(definition, "weixin_chat"), wantWeixin, definition)
	}
}

func assertWeixinOriginRejected(t *testing.T, ctx context.Context, conn *pgx.Conn, id string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, id); !isCheckViolation(err) {
		t.Fatalf("insert weixin_chat under pre-Weixin constraint: got %v, want check violation", err)
	}
}
