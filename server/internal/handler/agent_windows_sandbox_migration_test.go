package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAgentWindowsSandboxManagedMigrationRoundTrip(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE agent (
		id text PRIMARY KEY,
		custom_args jsonb NOT NULL DEFAULT '[]'::jsonb
	)`); err != nil {
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

	const sandbox = `windows.sandbox="unelevated"`
	rows := []struct {
		id      string
		args    []string
		managed bool
		want    []string
	}{
		{
			id:      "managed-suffix",
			args:    []string{"-c", sandbox, "--profile", "research"},
			managed: true,
			want:    []string{"--profile", "research"},
		},
		{
			id:      "managed-double-prefix",
			args:    []string{"-c", sandbox, "-c", sandbox, "--profile", "research"},
			managed: true,
			want:    []string{"-c", sandbox, "--profile", "research"},
		},
		{
			id:      "user-owned",
			args:    []string{"-c", sandbox, "--profile", "research"},
			managed: false,
			want:    []string{"-c", sandbox, "--profile", "research"},
		},
		{
			id:      "managed-not-leading",
			args:    []string{"--profile", "research", "-c", sandbox},
			managed: true,
			want:    []string{"--profile", "research", "-c", sandbox},
		},
		{
			id:      "managed-elevated",
			args:    []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
			managed: true,
			want:    []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
	}
	for _, row := range rows {
		raw, err := json.Marshal(row.args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent (id, custom_args, is_codex_windows_sandbox_arg_managed)
			VALUES ($1, $2::jsonb, $3)
		`, row.id, string(raw), row.managed); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
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

	assertArgs := func(id string, want []string) {
		t.Helper()
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT custom_args FROM agent WHERE id = $1`, id).Scan(&raw); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		var got []string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode %s custom_args: %v", id, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s custom_args = %v, want %v", id, got, want)
		}
	}
	for _, row := range rows {
		assertArgs(row.id, row.want)
	}

	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("reapply migration up after down: %v", err)
	}
	for _, row := range rows {
		assertArgs(row.id, row.want)
		var managed bool
		if err := tx.QueryRow(ctx, `
			SELECT is_codex_windows_sandbox_arg_managed FROM agent WHERE id = $1
		`, row.id).Scan(&managed); err != nil {
			t.Fatalf("read %s provenance after re-up: %v", row.id, err)
		}
		if managed {
			t.Fatalf("%s provenance after re-up = true, want safe user-owned default", row.id)
		}
	}
}
