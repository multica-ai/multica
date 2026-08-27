package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChannelOutboundMessageDownRefusesExplicitChannelChat(t *testing.T) {
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

	schema := "channel_route_down_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	if _, err := conn.Exec(ctx, `
		CREATE TABLE chat_session (
			id UUID PRIMARY KEY,
			explicitly_created_at TIMESTAMPTZ
		);
		CREATE TABLE channel_chat_session_binding (
			chat_session_id UUID NOT NULL,
			retired_at TIMESTAMPTZ
		);
		CREATE TABLE channel_outbound_message (id UUID);
		INSERT INTO chat_session (id, explicitly_created_at)
		VALUES ('22222222-2222-2222-2222-222222222222', now());
		INSERT INTO channel_chat_session_binding (chat_session_id, retired_at)
		VALUES ('22222222-2222-2222-2222-222222222222', NULL);
	`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	if _, err := conn.Exec(ctx, readMigrationFile(t, "425_channel_outbound_message.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back channel chat routes") {
		t.Fatalf("down migration error = %v, want explicit channel Chat refusal", err)
	}
	var outboundExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('channel_outbound_message') IS NOT NULL`).Scan(&outboundExists); err != nil {
		t.Fatalf("check preserved outbound table: %v", err)
	}
	if !outboundExists {
		t.Fatal("down migration dropped outbound table despite refused rollback")
	}

	if _, err := conn.Exec(ctx, `UPDATE chat_session SET explicitly_created_at = NULL`); err != nil {
		t.Fatalf("clear explicit origin: %v", err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "425_channel_outbound_message.down.sql")
	if err := conn.QueryRow(ctx, `SELECT to_regclass('channel_outbound_message') IS NOT NULL`).Scan(&outboundExists); err != nil {
		t.Fatalf("check dropped outbound table: %v", err)
	}
	if outboundExists {
		t.Fatal("down migration left outbound table without channel route state")
	}
}
