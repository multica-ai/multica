package handler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentWindowsSandboxManagedMigrationRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE agent (id integer)`); err != nil {
		t.Fatalf("create temporary agent table: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = pg_temp`); err != nil {
		t.Fatalf("select temporary schema: %v", err)
	}

	readMigration := func(name string) string {
		t.Helper()
		body, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil {
			t.Fatalf("read migration %s: %v", name, readErr)
		}
		return string(body)
	}
	up := readMigration("327_agent_is_codex_windows_sandbox_arg_managed.up.sql")
	down := readMigration("327_agent_is_codex_windows_sandbox_arg_managed.down.sql")

	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("apply migration up: %v", err)
	}
	var notNull bool
	var defaultExpression *string
	if err := tx.QueryRow(ctx, `
		SELECT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d
		  ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = 'agent'::regclass
		  AND a.attname = 'is_codex_windows_sandbox_arg_managed'
		  AND NOT a.attisdropped
	`).Scan(&notNull, &defaultExpression); err != nil {
		t.Fatalf("inspect migrated column: %v", err)
	}
	if !notNull || defaultExpression == nil || *defaultExpression != "false" {
		t.Fatalf("migrated column constraints = notNull:%v default:%v", notNull, defaultExpression)
	}

	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("apply migration down: %v", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_attribute
			WHERE attrelid = 'agent'::regclass
			  AND attname = 'is_codex_windows_sandbox_arg_managed'
			  AND NOT attisdropped
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("inspect rolled-back column: %v", err)
	}
	if exists {
		t.Fatal("down migration left is_codex_windows_sandbox_arg_managed behind")
	}

	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("reapply migration up after down: %v", err)
	}
}
