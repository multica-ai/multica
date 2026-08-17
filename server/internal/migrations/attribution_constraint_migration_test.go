package migrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	originatorID = "00000000-0000-0000-0000-000000000001"
	otherUserID  = "00000000-0000-0000-0000-000000000002"
)

func TestAttributionStrictConstraintMigrations(t *testing.T) {
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

	if _, err := conn.Exec(ctx, `
		CREATE TEMP TABLE agent_task_queue (
			id UUID PRIMARY KEY,
			originator_source TEXT NULL,
			originator_user_id UUID NULL,
			accountable_user_id UUID NULL
		)
	`); err != nil {
		t.Fatalf("create temporary queue table: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "190_agent_task_attribution_invariant_check.up.sql")

	// Migration 190 allows this pre-backfill shape solely because its source is
	// NULL. The strict shadow must refuse to validate until the row is repaired.
	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_task_queue (id, originator_user_id)
		VALUES ('00000000-0000-0000-0000-000000000010', $1)
	`, originatorID); err != nil {
		t.Fatalf("insert transitional legacy row: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "197_agent_task_attribution_strict_constraint.up.sql")
	validateSQL := readMigrationFile(t, "198_agent_task_attribution_strict_constraint_validate.up.sql")
	if _, err := conn.Exec(ctx, validateSQL); !isCheckViolation(err) {
		t.Fatalf("validate strict constraint with legacy row: got %v, want check violation", err)
	}

	if _, err := conn.Exec(ctx, `
		UPDATE agent_task_queue
		SET accountable_user_id = originator_user_id,
			originator_source = 'backfill'
		WHERE id = '00000000-0000-0000-0000-000000000010'
	`); err != nil {
		t.Fatalf("backfill transitional row: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "198_agent_task_attribution_strict_constraint_validate.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "199_agent_task_attribution_legacy_exemption_remove.up.sql")

	var validated bool
	var definition string
	if err := conn.QueryRow(ctx, `
		SELECT convalidated, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'agent_task_queue'::regclass
		  AND conname = 'agent_task_queue_accountable_matches_originator'
	`).Scan(&validated, &definition); err != nil {
		t.Fatalf("read final constraint: %v", err)
	}
	if !validated {
		t.Fatal("final attribution constraint is not validated")
	}
	if strings.Contains(definition, "originator_source") {
		t.Fatalf("final constraint still contains legacy exemption: %s", definition)
	}

	assertInsertCheckViolation(t, ctx, conn.Conn(), `
		INSERT INTO agent_task_queue (id, originator_source, originator_user_id)
		VALUES ('00000000-0000-0000-0000-000000000011', NULL, $1)
	`, originatorID)
	assertInsertCheckViolation(t, ctx, conn.Conn(), `
		INSERT INTO agent_task_queue (
			id, originator_source, originator_user_id, accountable_user_id
		) VALUES ('00000000-0000-0000-0000-000000000012', NULL, $1, $2)
	`, originatorID, otherUserID)

	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_task_queue (
			id, originator_source, originator_user_id, accountable_user_id
		) VALUES ('00000000-0000-0000-0000-000000000013', NULL, $1, $1)
	`, originatorID); err != nil {
		t.Fatalf("insert matching attribution under strict constraint: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "199_agent_task_attribution_legacy_exemption_remove.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "198_agent_task_attribution_strict_constraint_validate.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "197_agent_task_attribution_strict_constraint.down.sql")

	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_task_queue (id, originator_user_id)
		VALUES ('00000000-0000-0000-0000-000000000014', $1)
	`, originatorID); err != nil {
		t.Fatalf("insert legacy row after rollback restored exemption: %v", err)
	}
}

func applyMigrationFile(t *testing.T, ctx context.Context, conn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, name string) {
	t.Helper()
	if _, err := conn.Exec(ctx, readMigrationFile(t, name)); err != nil {
		t.Fatalf("apply migration %s: %v", name, err)
	}
}

func readMigrationFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(realMigrationsDir(t), name))
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(contents)
}

func assertInsertCheckViolation(t *testing.T, ctx context.Context, conn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(ctx, sql, args...); !isCheckViolation(err) {
		t.Fatalf("insert under strict constraint: got %v, want check violation", err)
	}
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
