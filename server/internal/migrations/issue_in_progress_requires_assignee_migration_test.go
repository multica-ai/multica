package migrations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// I4127.DP / I4192.DP: migration 349 enforces the in_progress-requires-
// assignee invariant at the DB level. This test runs the migration against
// the real schema inside a transaction that is rolled back afterwards, so
// the shared test database is left exactly as it was: a legacy zombie row is
// repaired to backlog by the migration itself, and the new CHECK refuses any
// subsequent write that would leave an issue in the in_progress category
// without an assignee — while assigned issues and other statuses stay open.
func TestIssueInProgressRequiresAssigneeMigration(t *testing.T) {
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

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx) // everything below, including the DDL, is undone

	// A scratch workspace so the FK on issue.workspace_id is satisfied.
	suffix := time.Now().UnixNano()
	var workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ($1, $2, $3)
		RETURNING id
	`, fmt.Sprintf("mig349 %d", suffix), fmt.Sprintf("mig349-%d", suffix), "M349").Scan(&workspaceID); err != nil {
		t.Fatalf("create scratch workspace: %v", err)
	}

	issueNumber := int32(0)
	insertIssue := func(status string, assigneeType, assigneeID *string) error {
		issueNumber++
		args := []any{workspaceID, fmt.Sprintf("issue %d %s", suffix, status), status, "member", issueNumber}
		var atCol, aidCol string
		if assigneeType != nil {
			atCol = ", assignee_type"
			aidCol = ", assignee_id"
			args = append(args, *assigneeType, *assigneeID)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number`+atCol+aidCol+`)
			VALUES ($1, $2, $3, $4, '00000000-0000-0000-0000-000000000001', $5`+assigneePlaceholders(assigneeType)+`)
		`, args...)
		return err
	}

	// mustViolate runs fn inside a savepoint and asserts it fails with a
	// CHECK violation, rolling back to the savepoint so the surrounding
	// transaction stays usable (a 23514 would otherwise abort the whole tx).
	mustViolate := func(name string, fn func() error) {
		t.Helper()
		if _, err := tx.Exec(ctx, `SAVEPOINT mig349_expect_check`); err != nil {
			t.Fatalf("%s: savepoint: %v", name, err)
		}
		err := fn()
		if !isCheckViolation(err) {
			t.Fatalf("%s: got %v, want check violation", name, err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT mig349_expect_check`); err != nil {
			t.Fatalf("%s: rollback to savepoint: %v", name, err)
		}
	}

	// A legacy zombie (in_progress, no assignee) can still be inserted BEFORE
	// the migration runs — that is exactly the gap 349 closes.
	if err := insertIssue("in_progress", nil, nil); err != nil {
		t.Fatalf("insert legacy zombie pre-migration: %v", err)
	}

	applyMigrationFile(t, ctx, tx, "349_issue_in_progress_requires_assignee.up.sql")

	// The migration repaired the legacy zombie to backlog.
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM issue WHERE workspace_id = $1`, workspaceID).Scan(&status); err != nil {
		t.Fatalf("read repaired issue: %v", err)
	}
	if status != "backlog" {
		t.Fatalf("legacy zombie after migration: got status %q, want backlog", status)
	}

	// Post-migration writes: in_progress without assignee is a CHECK
	// violation; in_progress with an assignee and unassigned todo are fine.
	mustViolate("insert in_progress without assignee", func() error {
		return insertIssue("in_progress", nil, nil)
	})
	agentID := "00000000-0000-0000-0000-000000000002"
	if err := insertIssue("in_progress", strPtr("agent"), &agentID); err != nil {
		t.Fatalf("insert in_progress with assignee: %v", err)
	}
	if err := insertIssue("todo", nil, nil); err != nil {
		t.Fatalf("insert unassigned todo: %v", err)
	}

	// The guard follows the EFFECTIVE status (MUL-6243): a custom status in
	// the in_progress category is held to the same rule.
	progressKey := fmt.Sprintf("mig349_progress_%d", suffix%1_000_000)
	if _, err := tx.Exec(ctx, `
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
		VALUES ($1, $2, $3, '', 'in_progress', '#123456', 1)
	`, workspaceID, progressKey, progressKey); err != nil {
		t.Fatalf("create custom in_progress-category status: %v", err)
	}
	mustViolate("insert custom in_progress-category without assignee", func() error {
		_, err := tx.Exec(ctx, `
			INSERT INTO issue (workspace_id, title, status, creator_type, creator_id)
			VALUES ($1, $2, $3, 'member', '00000000-0000-0000-0000-000000000001')
		`, workspaceID, "custom zombie", progressKey)
		return err
	})
}

func assigneePlaceholders(assigneeType *string) string {
	if assigneeType == nil {
		return ""
	}
	return ", $6, $7"
}

func strPtr(s string) *string { return &s }
