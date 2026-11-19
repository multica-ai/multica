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

	applyMigrationFile(t, ctx, conn.Conn(), "695_issue_origin_sharecrm_chat.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), false, true, true)
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'sharecrm_chat')`, "00000000-0000-4000-8000-000000000002"); err != nil {
		t.Fatalf("insert sharecrm_chat after widening constraint: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, "00000000-0000-4000-8000-000000000003"); err != nil {
		t.Fatalf("insert weixin_chat after ShareCRM widening: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "696_issue_origin_sharecrm_chat_validate.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)

	applyMigrationFile(t, ctx, conn.Conn(), "696_issue_origin_sharecrm_chat_validate.down.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), false, true, true)
	if _, err := conn.Exec(ctx, `DELETE FROM issue WHERE origin_type = 'sharecrm_chat'`); err != nil {
		t.Fatalf("remove ShareCRM row before narrowing rollback: %v", err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "695_issue_origin_sharecrm_chat.down.sql")
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
	applyMigrationFile(t, ctx, conn.Conn(), "695_issue_origin_sharecrm_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "696_issue_origin_sharecrm_chat_validate.up.sql")
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

func TestStuckUpstream667RestoresExistingOrigins(t *testing.T) {
	ctx, conn := openOriginFixture(t, "sharecrm_origin_stuck_667")

	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue (id UUID PRIMARY KEY, origin_type TEXT NULL);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
	`); err != nil {
		t.Fatalf("create pre-upgrade issue table: %v", err)
	}
	for _, row := range []struct{ id, origin string }{
		{"00000000-0000-4000-8000-000000000021", "sharecrm_chat"},
		{"00000000-0000-4000-8000-000000000022", "custom_origin"},
		{"00000000-0000-4000-8000-000000000023", "weird'origin"},
	} {
		if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, $2)`, row.id, row.origin); err != nil {
			t.Fatalf("insert %s: %v", row.origin, err)
		}
	}

	// The 667 file an earlier checkout of this PR already committed: weixin
	// only, NOT VALID, so existing rows remain and new ShareCRM writes fail.
	if _, err := conn.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat'
			))
			NOT VALID;
	`); err != nil {
		t.Fatalf("apply already-committed upstream 667: %v", err)
	}
	assertShareCRMOriginRejected(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000024")

	applyMigrationFile(t, ctx, conn.Conn(), "668_issue_origin_weixin_chat_validate.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)
	for _, row := range []struct{ id, origin string }{
		{"00000000-0000-4000-8000-000000000021", "sharecrm_chat"},
		{"00000000-0000-4000-8000-000000000022", "custom_origin"},
		{"00000000-0000-4000-8000-000000000023", "weird'origin"},
	} {
		assertIssueOrigin(t, ctx, conn.Conn(), row.id, row.origin)
	}
	for _, row := range []struct{ id, origin string }{
		{"00000000-0000-4000-8000-000000000025", "sharecrm_chat"},
		{"00000000-0000-4000-8000-000000000026", "weixin_chat"},
		{"00000000-0000-4000-8000-000000000027", "custom_origin"},
		{"00000000-0000-4000-8000-000000000028", "weird'origin"},
	} {
		if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, $2)`, row.id, row.origin); err != nil {
			t.Fatalf("insert %s after 668 repair: %v", row.origin, err)
		}
	}

	applyMigrationFile(t, ctx, conn.Conn(), "695_issue_origin_sharecrm_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "696_issue_origin_sharecrm_chat_validate.up.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)
	if !originAllowed(t, ctx, conn.Conn(), "custom_origin") || !originAllowed(t, ctx, conn.Conn(), "weird'origin") {
		t.Fatal("repair lost an origin that still had rows")
	}
}

func TestOriginNarrowingRollsBackWhileRowsRemain(t *testing.T) {
	ctx, conn := openOriginFixture(t, "sharecrm_origin_rollback")
	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue (id UUID PRIMARY KEY, origin_type TEXT NULL);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat', 'custom_origin'
			));
	`); err != nil {
		t.Fatalf("create issue table: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'sharecrm_chat')`, "00000000-0000-4000-8000-000000000031"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'custom_origin')`, "00000000-0000-4000-8000-000000000032"); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "667_issue_origin_weixin_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "668_issue_origin_weixin_chat_validate.up.sql")
	if _, err := conn.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, 'weixin_chat')`, "00000000-0000-4000-8000-000000000033"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, readMigrationFile(t, "667_issue_origin_weixin_chat.down.sql")); !isCheckViolation(err) {
		t.Fatalf("667 down with a weixin row: got %v, want check violation", err)
	}
	assertIssueOrigin(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000033", "weixin_chat")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)
	if !originAllowed(t, ctx, conn.Conn(), "custom_origin") {
		t.Fatal("failed 667 down dropped custom_origin")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM issue WHERE origin_type = 'weixin_chat'`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "667_issue_origin_weixin_chat.down.sql")
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, false, true)
	assertIssueOrigin(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000031", "sharecrm_chat")
	assertIssueOrigin(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000032", "custom_origin")

	applyMigrationFile(t, ctx, conn.Conn(), "667_issue_origin_weixin_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "668_issue_origin_weixin_chat_validate.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "695_issue_origin_sharecrm_chat.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "696_issue_origin_sharecrm_chat_validate.up.sql")
	if _, err := conn.Exec(ctx, readMigrationFile(t, "695_issue_origin_sharecrm_chat.down.sql")); !isCheckViolation(err) {
		t.Fatalf("683 down with a sharecrm row: got %v, want check violation", err)
	}
	assertShareCRMOriginConstraint(t, ctx, conn.Conn(), true, true, true)
	assertIssueOrigin(t, ctx, conn.Conn(), "00000000-0000-4000-8000-000000000031", "sharecrm_chat")
}

func originAllowed(t *testing.T, ctx context.Context, conn *pgx.Conn, origin string) bool {
	t.Helper()
	var definition string
	if err := conn.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = to_regclass('issue') AND conname = 'issue_origin_type_check'
	`).Scan(&definition); err != nil {
		t.Fatalf("read origin constraint: %v", err)
	}
	return strings.Contains(definition, "'"+strings.ReplaceAll(origin, "'", "''")+"'")
}

func openOriginFixture(t *testing.T, schema string) (context.Context, *pgxpool.Conn) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatalf("acquire Postgres connection: %v", err)
	}
	t.Cleanup(func() {
		conn.Release()
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		pool.Close()
	})
	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatal(err)
	}
	return ctx, conn
}
