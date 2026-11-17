package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sharecrmOriginMigrationTestSchema = "sharecrm_origin_migration_test"

func TestShareCRMOriginMigrationsUpDownAndCatalog(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+sharecrmOriginMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+sharecrmOriginMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, sharecrmOriginMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue (
			id UUID PRIMARY KEY,
			origin_type TEXT NULL
		);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat'));
	`); err != nil {
		t.Fatalf("create post-WeChat issue table: %v", err)
	}

	assertShareCRMOriginRejected(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000001")

	applyMigrationFile(t, ctx, conn.Conn(), "673_issue_origin_sharecrm_chat.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), false, true, true)
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'sharecrm_chat')`, "00000000-0000-4000-8000-000000000002"); err != nil {
		t.Fatalf("insert sharecrm_chat after widening constraint: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, "00000000-0000-4000-8000-000000000003"); err != nil {
		t.Fatalf("insert weixin_chat after ShareCRM widening: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "674_issue_origin_sharecrm_chat_validate.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)

	applyMigrationFile(t, ctx, conn.Conn(), "674_issue_origin_sharecrm_chat_validate.down.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), false, true, true)
	if _, err := conn.Exec(ctx, `DELETE FROM issue WHERE origin_type = 'sharecrm_chat'`); err != nil {
		t.Fatalf("remove ShareCRM row before narrowing rollback: %v", err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "673_issue_origin_sharecrm_chat.down.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, false)
	assertShareCRMOriginRejected(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000004")
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, "00000000-0000-4000-8000-000000000005"); err != nil {
		t.Fatalf("insert weixin_chat after ShareCRM rollback: %v", err)
	}
}

func assertShareCRMOriginConstraint(t *testing.T, ctx context.Context, conn *pgx.Conn, wantValidated, wantWeixin, wantShareCRM bool) {
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
	if strings.Contains(definition, "sharecrm_chat") != wantShareCRM {
		t.Fatalf("constraint ShareCRM membership = %t, want %t: %s", strings.Contains(definition, "sharecrm_chat"), wantShareCRM, definition)
	}
}

func TestShareCRMOriginSurvivesWeChatUpgrade(t *testing.T) {
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

	const schema = "sharecrm_origin_upgrade_test"
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	// Previous ShareCRM release: migrations 665/666 already validated
	// sharecrm_chat, and WeChat migrations 667/668 have not run.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue (
			id UUID PRIMARY KEY,
			origin_type TEXT NULL
		);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat'));
	`); err != nil {
		t.Fatalf("create pre-WeChat ShareCRM issue table: %v", err)
	}
	const sharecrmIssue = "00000000-0000-4000-8000-000000000010"
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'sharecrm_chat')`, sharecrmIssue); err != nil {
		t.Fatalf("insert existing sharecrm_chat issue: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "667_issue_origin_weixin_chat.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), false, true, true)
	applyMigrationFile(t, ctx, conn.Conn(), "668_issue_origin_weixin_chat_validate.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)
	assertIssueOrigin(t, ctx, conn.Conn(), sharecrmIssue, "sharecrm_chat")

	applyMigrationFile(t, ctx, conn.Conn(), "668_issue_origin_weixin_chat_validate.down.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), false, true, true)
	applyMigrationFile(t, ctx, conn.Conn(), "667_issue_origin_weixin_chat.down.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, false, true)
	assertIssueOrigin(t, ctx, conn.Conn(), sharecrmIssue, "sharecrm_chat")
	assertWeixinInsertRejected(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000011")

	applyMigrationFile(t, ctx, conn.Conn(), "667_issue_origin_weixin_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "668_issue_origin_weixin_chat_validate.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "673_issue_origin_sharecrm_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "674_issue_origin_sharecrm_chat_validate.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)
	assertIssueOrigin(t, ctx, conn.Conn(), sharecrmIssue, "sharecrm_chat")
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, "00000000-0000-4000-8000-000000000012"); err != nil {
		t.Fatalf("insert weixin_chat after ShareCRM upgrade: %v", err)
	}
}

func assertIssueOrigin(t *testing.T, ctx context.Context, conn *pgx.Conn, id, origin string) {
	t.Helper()
	var got string
	if err := conn.QueryRow(ctx, `SELECT origin_type FROM issue WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read issue %s origin: %v", id, err)
	}
	if got != origin {
		t.Fatalf("issue %s origin = %q, want %q", id, got, origin)
	}
}

func assertWeixinInsertRejected(t *testing.T, ctx context.Context, conn *pgx.Conn, id string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, id); !isCheckViolation(err) {
		t.Fatalf("insert weixin_chat under pre-Weixin constraint: got %v, want check violation", err)
	}
}

func assertShareCRMOriginRejected(t *testing.T, ctx context.Context, conn *pgx.Conn, id string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'sharecrm_chat')`, id); !isCheckViolation(err) {
		t.Fatalf("insert sharecrm_chat under pre-ShareCRM constraint: got %v, want check violation", err)
	}
}
