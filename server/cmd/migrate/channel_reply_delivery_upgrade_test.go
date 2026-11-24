package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A database that installed channel_reply_delivery before attempt_depth
// existed must gain the column by upgrading, not by being rebuilt.
//
// The column was first added by editing the create-table migration in place.
// Any database
// that had already recorded 500 never saw the change, so every query touching
// the column failed on exactly the deployments where the feature was already
// installed — and a fresh rebuild passing said nothing about it. This runs the
// upgrade the way a deployment does: the old table shape, then the migration.
func TestChannelReplyDeliveryAttemptDepthUpgradesAnInstalledTable(t *testing.T) {
	t.Parallel()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	schema := "reply_delivery_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}

	// The table as the create-table migration made it, before the column existed.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE channel_reply_delivery (
			turn_id UUID NOT NULL,
			task_id UUID NOT NULL,
			binding_id UUID NOT NULL,
			installation_id UUID NOT NULL,
			channel_type TEXT NOT NULL,
			chat_id TEXT NOT NULL,
			phase TEXT NOT NULL,
			send_state TEXT NOT NULL,
			message_id TEXT NOT NULL DEFAULT '',
			chunks_sent INTEGER NOT NULL DEFAULT 0,
			settled_reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		t.Fatalf("create the pre-upgrade table: %v", err)
	}
	// A row written by the already-installed version, to prove the upgrade
	// keeps existing turns rather than requiring an empty table.
	turnID := uuid.NewString()
	if _, err := conn.Exec(ctx, `
		INSERT INTO channel_reply_delivery (
			turn_id, task_id, binding_id, installation_id, channel_type, chat_id, phase, send_state
		) VALUES ($1, $1, $1, $1, 'telegram', '42', 'streaming', 'none')
	`, turnID); err != nil {
		t.Fatalf("seed an installed row: %v", err)
	}

	upgrade, err := os.ReadFile("../../migrations/506_channel_reply_delivery_attempt_depth.up.sql")
	if err != nil {
		t.Fatalf("read the upgrade migration: %v", err)
	}
	if _, err := conn.Exec(ctx, string(upgrade)); err != nil {
		t.Fatalf("apply the upgrade migration: %v", err)
	}

	var depth int
	if err := conn.QueryRow(ctx,
		`SELECT attempt_depth FROM channel_reply_delivery WHERE turn_id = $1`, turnID).Scan(&depth); err != nil {
		t.Fatalf("read attempt_depth after the upgrade: %v", err)
	}
	if depth != 0 {
		t.Fatalf("existing turn got attempt_depth %d, want the 0 default", depth)
	}

	// The constraint has to exist too, or an upgraded database and a fresh one
	// disagree about what the column accepts.
	var constraints int
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint
		WHERE conname = 'channel_reply_delivery_attempt_depth_check'
		  AND connamespace = $1::regnamespace
	`, schema).Scan(&constraints); err != nil {
		t.Fatalf("look up the depth constraint: %v", err)
	}
	if constraints != 1 {
		t.Fatalf("upgraded schema has %d attempt_depth constraints, want 1", constraints)
	}

	// Applying it twice must be a no-op: migrations are retried after a failed
	// run, and a conditionally skipped one is still recorded.
	if _, err := conn.Exec(ctx, string(upgrade)); err != nil {
		t.Fatalf("re-apply the upgrade migration: %v", err)
	}
}
