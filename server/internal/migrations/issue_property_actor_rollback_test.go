package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIssuePropertyActorRollbackFailsClosed pins the one behaviour a production
// rollback depends on: 341 down must refuse to run while actor definitions
// exist, rather than deleting them. A definition row is the field itself —
// issues key their values by definition id — so a silent delete orphans every
// value and the up migration cannot restore it.
func TestIssuePropertyActorRollbackFailsClosed(t *testing.T) {
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

	// Temp table shadows the real catalog for this session, so the migration
	// files run unmodified against a disposable copy of the shape they target.
	if _, err := conn.Exec(ctx, `
		CREATE TEMP TABLE issue_property (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			archived_at TIMESTAMPTZ,
			CONSTRAINT issue_property_type_check CHECK (
				type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url')
			)
		)
	`); err != nil {
		t.Fatalf("create temporary issue_property table: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "341_issue_property_actor_types.up.sql")

	const workspaceID = "00000000-0000-0000-0000-000000000001"
	if _, err := conn.Exec(ctx, `
		INSERT INTO issue_property (workspace_id, name, type)
		VALUES ($1, 'Reviewer', 'actor'), ($1, 'Escalation', 'multi_actor')
	`, workspaceID); err != nil {
		t.Fatalf("insert actor property definitions: %v", err)
	}
	// Archived definitions still resolve historical values, so they must block
	// the rollback exactly like active ones.
	if _, err := conn.Exec(ctx, `
		INSERT INTO issue_property (workspace_id, name, type, archived_at)
		VALUES ($1, 'Old reviewer', 'actor', now())
	`, workspaceID); err != nil {
		t.Fatalf("insert archived actor property definition: %v", err)
	}

	downSQL := readMigrationFile(t, "341_issue_property_actor_types.down.sql")
	_, err = conn.Exec(ctx, downSQL)
	if err == nil {
		t.Fatal("down migration succeeded with actor definitions present; it must fail closed")
	}
	if !strings.Contains(err.Error(), "cannot roll back 341") {
		t.Fatalf("down migration failed for the wrong reason: %v", err)
	}

	var remaining int
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FROM issue_property WHERE type IN ('actor', 'multi_actor')
	`).Scan(&remaining); err != nil {
		t.Fatalf("count actor definitions after refused rollback: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("refused rollback destroyed definitions: %d of 3 left", remaining)
	}

	// After the operator migrates those definitions off the actor types, the
	// rollback goes through and the pre-actor allowlist is back in force.
	if _, err := conn.Exec(ctx, `
		UPDATE issue_property SET type = 'text' WHERE type IN ('actor', 'multi_actor')
	`); err != nil {
		t.Fatalf("convert actor definitions to text: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "341_issue_property_actor_types.down.sql")

	if _, err := conn.Exec(ctx, `
		INSERT INTO issue_property (workspace_id, name, type)
		VALUES ($1, 'Reviewer again', 'actor')
	`, workspaceID); !isCheckViolation(err) {
		t.Fatalf("insert actor definition after rollback: got %v, want check violation", err)
	}
}
