package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const issueCommentRevisionMigrationTestSchema = "issue_comment_revision_migration_test"

func TestIssueCommentRevisionMigrationUpDownPreservesData(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+issueCommentRevisionMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+issueCommentRevisionMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, issueCommentRevisionMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue (id TEXT PRIMARY KEY, title TEXT NOT NULL);
		CREATE TABLE comment (id TEXT PRIMARY KEY, issue_id TEXT NOT NULL, content TEXT NOT NULL);
		INSERT INTO issue (id, title) VALUES ('issue-1', 'Original title');
		INSERT INTO comment (id, issue_id, content) VALUES ('comment-1', 'issue-1', 'Original comment');
	`); err != nil {
		t.Fatalf("create pre-migration fixtures: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "351_issue_comment_revision.up.sql")
	assertRevisionColumn(t, ctx, conn.Conn(), "issue")
	assertRevisionColumn(t, ctx, conn.Conn(), "comment")
	assertRevisionValues(t, ctx, conn.Conn(), 1, 1)

	if _, err := conn.Exec(ctx, `
		INSERT INTO issue (id, title) VALUES ('issue-2', 'Post-migration title');
		INSERT INTO comment (id, issue_id, content) VALUES ('comment-2', 'issue-2', 'Post-migration comment');
	`); err != nil {
		t.Fatalf("insert rows using revision defaults: %v", err)
	}
	assertRevisionValues(t, ctx, conn.Conn(), 2, 2)

	applyMigrationFile(t, ctx, conn.Conn(), "351_issue_comment_revision.down.sql")
	assertRevisionColumnMissing(t, ctx, conn.Conn(), "issue")
	assertRevisionColumnMissing(t, ctx, conn.Conn(), "comment")

	var issueCount, commentCount int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM issue), (SELECT count(*) FROM comment)`).Scan(&issueCount, &commentCount); err != nil {
		t.Fatalf("count rows after down migration: %v", err)
	}
	if issueCount != 2 || commentCount != 2 {
		t.Fatalf("row counts after down migration = (%d, %d), want (2, 2)", issueCount, commentCount)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "351_issue_comment_revision.up.sql")
	assertRevisionValues(t, ctx, conn.Conn(), 2, 2)
}

func assertRevisionColumn(t *testing.T, ctx context.Context, conn *pgx.Conn, table string) {
	t.Helper()
	var nullable, defaultValue string
	if err := conn.QueryRow(ctx, `
		SELECT is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = 'revision'
	`, issueCommentRevisionMigrationTestSchema, table).Scan(&nullable, &defaultValue); err != nil {
		t.Fatalf("inspect %s.revision: %v", table, err)
	}
	if nullable != "NO" || defaultValue != "1" {
		t.Fatalf("%s.revision metadata = (nullable %s, default %s), want (NO, 1)", table, nullable, defaultValue)
	}
}

func assertRevisionColumnMissing(t *testing.T, ctx context.Context, conn *pgx.Conn, table string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = 'revision'
	`, issueCommentRevisionMigrationTestSchema, table).Scan(&count); err != nil {
		t.Fatalf("inspect missing %s.revision: %v", table, err)
	}
	if count != 0 {
		t.Fatalf("%s.revision still exists after down migration", table)
	}
}

func assertRevisionValues(t *testing.T, ctx context.Context, conn *pgx.Conn, wantIssues, wantComments int) {
	t.Helper()
	var issues, comments int
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM issue WHERE revision = 1),
			(SELECT count(*) FROM comment WHERE revision = 1)
	`).Scan(&issues, &comments); err != nil {
		t.Fatalf("read revision values: %v", err)
	}
	if issues != wantIssues || comments != wantComments {
		t.Fatalf("revision=1 counts = (%d, %d), want (%d, %d)", issues, comments, wantIssues, wantComments)
	}
}
