package migrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigration195RepairsLegacyCompositeWorkspaceConstraint(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL is required for the DB-backed migration test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	schema := pgx.Identifier{fmt.Sprintf("migration_195_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE issue (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			CONSTRAINT uq_issue_workspace_id UNIQUE(workspace_id, id)
		);
		CREATE TABLE issue_external_identity (
			workspace_id UUID NOT NULL,
			namespace TEXT NOT NULL,
			external_id TEXT NOT NULL,
			issue_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(workspace_id, namespace, external_id),
			CONSTRAINT issue_external_identity_issue_fk
				FOREIGN KEY(workspace_id, issue_id)
				REFERENCES issue(workspace_id, id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatalf("create legacy migration 194 shape: %v", err)
	}

	const issueID = "33333333-3333-3333-3333-333333333333"
	const workspaceID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const otherWorkspace = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	if _, err := tx.Exec(ctx, `INSERT INTO issue(id, workspace_id) VALUES($1, $2)`, issueID, workspaceID); err != nil {
		t.Fatalf("insert legacy issue: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id) VALUES($1, 'github-node', 'legacy-node', $2)`, workspaceID, issueID); err != nil {
		t.Fatalf("insert legacy alias: %v", err)
	}

	up, err := os.ReadFile("195_issue_external_identity_workspace_guard.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply migration 195 up: %v", err)
	}

	var fkDefinition string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid='issue_external_identity'::regclass
		  AND conname='issue_external_identity_issue_fk'
	`).Scan(&fkDefinition); err != nil {
		t.Fatalf("read repaired foreign key: %v", err)
	}
	if !strings.Contains(fkDefinition, "FOREIGN KEY (issue_id) REFERENCES issue(id) ON DELETE CASCADE") || strings.Contains(fkDefinition, "workspace_id, issue_id") {
		t.Fatalf("repaired foreign key = %q", fkDefinition)
	}
	var broadConstraintCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='issue'::regclass AND conname='uq_issue_workspace_id'`).Scan(&broadConstraintCount); err != nil {
		t.Fatalf("inspect broad issue constraint: %v", err)
	}
	if broadConstraintCount != 0 {
		t.Fatalf("migration 195 left %d broad issue constraints", broadConstraintCount)
	}
	var aliasCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE issue_id=$1`, issueID).Scan(&aliasCount); err != nil {
		t.Fatalf("read preserved aliases: %v", err)
	}
	if aliasCount != 1 {
		t.Fatalf("preserved alias count = %d, want 1", aliasCount)
	}

	if _, err := tx.Exec(ctx, "SAVEPOINT workspace_move"); err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, moveErr := tx.Exec(ctx, `UPDATE issue SET workspace_id=$1 WHERE id=$2`, otherWorkspace, issueID)
	var pgErr *pgconn.PgError
	if !errors.As(moveErr, &pgErr) || pgErr.Code != "23503" || !strings.Contains(pgErr.Message, "external identity issue workspace cannot change") {
		t.Fatalf("workspace move error = %v, want repaired guard", moveErr)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT workspace_move"); err != nil {
		t.Fatalf("rollback expected move error: %v", err)
	}

	// Reapplying the repair is safe for preview/staging databases whose schema
	// history may include either supported shape of migration 194.
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("reapply migration 195 up: %v", err)
	}
	down, err := os.ReadFile("195_issue_external_identity_workspace_guard.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply migration 195 down: %v", err)
	}
	_, moveErr = tx.Exec(ctx, `UPDATE issue SET workspace_id=$1 WHERE id=$2`, otherWorkspace, issueID)
	if !errors.As(moveErr, &pgErr) || pgErr.Code != "23503" || !strings.Contains(pgErr.Message, "external identity issue workspace cannot change") {
		t.Fatalf("no-op rollback restored unsafe schema: %v", moveErr)
	}
}

func TestMigration195RefusesExistingWorkspaceMismatch(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL is required for the DB-backed migration test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	schema := pgx.Identifier{fmt.Sprintf("migration_195_mismatch_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE issue (id UUID PRIMARY KEY, workspace_id UUID NOT NULL);
		CREATE TABLE issue_external_identity (
			workspace_id UUID NOT NULL,
			namespace TEXT NOT NULL,
			external_id TEXT NOT NULL,
			issue_id UUID NOT NULL,
			PRIMARY KEY(workspace_id, namespace, external_id)
		);
		INSERT INTO issue VALUES ('44444444-4444-4444-4444-444444444444', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa');
		INSERT INTO issue_external_identity VALUES ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 'github-node', 'mismatched-node', '44444444-4444-4444-4444-444444444444');
	`); err != nil {
		t.Fatalf("seed mismatched legacy data: %v", err)
	}
	up, err := os.ReadFile("195_issue_external_identity_workspace_guard.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, applyErr := tx.Exec(ctx, string(up))
	var pgErr *pgconn.PgError
	if !errors.As(applyErr, &pgErr) || pgErr.Code != "23514" || !strings.Contains(pgErr.Message, "mismatched issue workspaces") {
		t.Fatalf("migration error = %v, want fail-closed mismatch validation", applyErr)
	}
}
