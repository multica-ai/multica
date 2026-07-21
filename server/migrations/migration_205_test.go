package migrations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestMigration205CreatesRelationshipFreeExternalIdentityTable(t *testing.T) {
	raw, err := os.ReadFile("205_issue_external_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{"foreign key", "references", "create trigger", "create index"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("migration 205 contains database-owned relationship/index component %q", forbidden)
		}
	}

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

	schemaName := fmt.Sprintf("migration_205_%d", time.Now().UnixNano())
	schema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("apply migration 205 up: %v", err)
	}

	var foreignKeys, userTriggers int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='issue_external_identity'::regclass AND contype='f'`).Scan(&foreignKeys); err != nil {
		t.Fatalf("inspect foreign keys: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='issue_external_identity'::regclass AND NOT tgisinternal`).Scan(&userTriggers); err != nil {
		t.Fatalf("inspect triggers: %v", err)
	}
	if foreignKeys != 0 || userTriggers != 0 {
		t.Fatalf("database-owned relationship artifacts: foreign_keys=%d triggers=%d", foreignKeys, userTriggers)
	}

	down, err := os.ReadFile("205_issue_external_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply migration 205 down: %v", err)
	}
	var table *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1)::text`, schemaName+".issue_external_identity").Scan(&table); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if table != nil {
		t.Fatalf("down migration left table %q", *table)
	}
}
