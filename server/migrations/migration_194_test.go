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

func TestMigration194OwnsOnlyItsObjectsAndDoesNotMutateIssueConstraints(t *testing.T) {
	upBytes, err := os.ReadFile("194_issue_external_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := os.ReadFile("194_issue_external_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down := strings.ToLower(string(upBytes)), strings.ToLower(string(downBytes))
	for _, forbidden := range []string{"uq_issue_workspace_id", "alter table issue", "unique (workspace_id, id)"} {
		if strings.Contains(up, forbidden) || strings.Contains(down, forbidden) {
			t.Fatalf("migration 194 contains destructive/pre-existing issue constraint operation %q", forbidden)
		}
	}
	for _, required := range []string{"references issue(id)", "issue_external_identity_workspace_180", "issue_external_identity_issue_workspace_180", "for share"} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 194 missing owned invariant component %q", required)
		}
	}
	for _, required := range []string{
		"drop trigger if exists issue_external_identity_issue_workspace_180",
		"drop trigger if exists issue_external_identity_workspace_180",
		"drop table if exists issue_external_identity",
		"drop function if exists issue_external_identity_guard_issue_workspace_180",
		"drop function if exists issue_external_identity_enforce_workspace_180",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("down migration missing owned-object cleanup %q", required)
		}
	}
}

func TestMigration194RejectsMovingAnIssueAwayFromItsAliasWorkspace(t *testing.T) {
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

	schemaName := fmt.Sprintf("migration_194_%d", time.Now().UnixNano())
	schema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE issue (id UUID PRIMARY KEY, workspace_id UUID NOT NULL)`); err != nil {
		t.Fatalf("create minimal issue table: %v", err)
	}
	up, err := os.ReadFile("194_issue_external_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply migration 194 up: %v", err)
	}

	const issueID = "11111111-1111-1111-1111-111111111111"
	const originalWorkspace = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const otherWorkspace = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	if _, err := tx.Exec(ctx, `INSERT INTO issue(id, workspace_id) VALUES($1, $2)`, issueID, originalWorkspace); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id) VALUES($1, 'github-node', 'node-1', $2)`, originalWorkspace, issueID); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT workspace_move"); err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, moveErr := tx.Exec(ctx, `UPDATE issue SET workspace_id=$1 WHERE id=$2`, otherWorkspace, issueID)
	var pgErr *pgconn.PgError
	if !errors.As(moveErr, &pgErr) || pgErr.Code != "23503" || !strings.Contains(pgErr.Message, "external identity issue workspace cannot change") {
		t.Fatalf("workspace move error = %v, want migration 194 same-workspace guard", moveErr)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT workspace_move"); err != nil {
		t.Fatalf("rollback expected error: %v", err)
	}
	var issueWorkspace, aliasWorkspace string
	if err := tx.QueryRow(ctx, `SELECT i.workspace_id::text, e.workspace_id::text FROM issue i JOIN issue_external_identity e ON e.issue_id=i.id WHERE i.id=$1`, issueID).Scan(&issueWorkspace, &aliasWorkspace); err != nil {
		t.Fatalf("read invariant: %v", err)
	}
	if issueWorkspace != originalWorkspace || aliasWorkspace != originalWorkspace {
		t.Fatalf("workspace invariant changed: issue=%s alias=%s", issueWorkspace, aliasWorkspace)
	}

	down, err := os.ReadFile("194_issue_external_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply migration 194 down: %v", err)
	}
	var aliasTable *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1)::text`, schemaName+".issue_external_identity").Scan(&aliasTable); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if aliasTable != nil {
		t.Fatalf("down migration left alias table %q", *aliasTable)
	}
	var ownedTriggerCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='issue'::regclass AND NOT tgisinternal AND tgname='issue_external_identity_issue_workspace_180'`).Scan(&ownedTriggerCount); err != nil {
		t.Fatalf("inspect issue trigger cleanup: %v", err)
	}
	if ownedTriggerCount != 0 {
		t.Fatalf("down migration left %d owned issue triggers", ownedTriggerCount)
	}
}

func TestMigration194SerializesAliasInsertWithIssueWorkspaceMove(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL is required for the DB-backed migration test")
	}
	ctx := context.Background()
	aliasConn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect alias session: %v", err)
	}
	defer aliasConn.Close(ctx)
	moveConn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect move session: %v", err)
	}
	defer moveConn.Close(ctx)

	schema := pgx.Identifier{fmt.Sprintf("migration_194_race_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := aliasConn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer aliasConn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	if _, err := aliasConn.Exec(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set alias search path: %v", err)
	}
	if _, err := moveConn.Exec(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set move search path: %v", err)
	}
	if _, err := aliasConn.Exec(ctx, `CREATE TABLE issue (id UUID PRIMARY KEY, workspace_id UUID NOT NULL)`); err != nil {
		t.Fatalf("create minimal issue table: %v", err)
	}
	up, err := os.ReadFile("194_issue_external_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aliasConn.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply migration 194 up: %v", err)
	}

	const issueID = "22222222-2222-2222-2222-222222222222"
	const originalWorkspace = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const otherWorkspace = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	if _, err := aliasConn.Exec(ctx, `INSERT INTO issue(id, workspace_id) VALUES($1, $2)`, issueID, originalWorkspace); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	aliasTx, err := aliasConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin alias insert: %v", err)
	}
	defer aliasTx.Rollback(ctx)
	if _, err := aliasTx.Exec(ctx, `INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id) VALUES($1, 'github-node', 'concurrent-node', $2)`, originalWorkspace, issueID); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	moveTx, err := moveConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace move: %v", err)
	}
	defer moveTx.Rollback(ctx)
	moveDone := make(chan error, 1)
	go func() {
		_, updateErr := moveTx.Exec(ctx, `UPDATE issue SET workspace_id=$1 WHERE id=$2`, otherWorkspace, issueID)
		if updateErr == nil {
			updateErr = moveTx.Commit(ctx)
		} else {
			_ = moveTx.Rollback(ctx)
		}
		moveDone <- updateErr
	}()

	blocked := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := aliasTx.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, int32(moveConn.PgConn().PID())).Scan(&blocked); err != nil {
			t.Fatalf("inspect concurrent lock: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("workspace move did not block behind the in-flight alias insert")
	}
	if err := aliasTx.Commit(ctx); err != nil {
		t.Fatalf("commit alias insert: %v", err)
	}
	moveErr := <-moveDone
	var pgErr *pgconn.PgError
	if !errors.As(moveErr, &pgErr) || pgErr.Code != "23503" || !strings.Contains(pgErr.Message, "external identity issue workspace cannot change") {
		t.Fatalf("concurrent workspace move error = %v, want migration 194 same-workspace guard", moveErr)
	}

	var issueWorkspace, aliasWorkspace string
	if err := aliasConn.QueryRow(ctx, `SELECT i.workspace_id::text, e.workspace_id::text FROM issue i JOIN issue_external_identity e ON e.issue_id=i.id WHERE i.id=$1`, issueID).Scan(&issueWorkspace, &aliasWorkspace); err != nil {
		t.Fatalf("read concurrent invariant: %v", err)
	}
	if issueWorkspace != originalWorkspace || aliasWorkspace != originalWorkspace {
		t.Fatalf("concurrent workspace invariant changed: issue=%s alias=%s", issueWorkspace, aliasWorkspace)
	}
}
