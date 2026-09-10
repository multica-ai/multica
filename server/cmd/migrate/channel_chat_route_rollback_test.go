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

func TestChannelChatRouteRollbackGuardRunsBeforeConcurrentIndexDrop(t *testing.T) {
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

	schema := "channel_route_rollback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
			id UUID PRIMARY KEY,
			chat_session_id UUID NOT NULL,
			retired_at TIMESTAMPTZ
		);
		CREATE TABLE channel_outbound_message (
			channel_message_id TEXT NOT NULL,
			binding_id UUID NOT NULL,
			route_revision BIGINT NOT NULL
		);
		CREATE INDEX idx_channel_outbound_message_binding_route
			ON channel_outbound_message(binding_id, route_revision);
		INSERT INTO chat_session (id, explicitly_created_at)
		VALUES ('22222222-2222-2222-2222-222222222222', NULL);
		INSERT INTO channel_chat_session_binding (id, chat_session_id, retired_at)
		VALUES (
			'11111111-1111-1111-1111-111111111111',
			'22222222-2222-2222-2222-222222222222',
			now()
		);
	`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	err = refuseChannelChatRouteHistoryRollbackWith(ctx, conn)
	if err == nil || !strings.Contains(err.Error(), "cannot roll back channel chat routes") {
		t.Fatalf("rollback error = %v, want route-history refusal", err)
	}
	var indexExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('idx_channel_outbound_message_binding_route') IS NOT NULL`).Scan(&indexExists); err != nil {
		t.Fatalf("check preserved index: %v", err)
	}
	if !indexExists {
		t.Fatal("concurrent index was dropped despite refused rollback")
	}

	if _, err := conn.Exec(ctx, `
		UPDATE channel_chat_session_binding SET retired_at = NULL;
		UPDATE chat_session SET explicitly_created_at = now();
	`); err != nil {
		t.Fatalf("replace route history with an explicitly created active route: %v", err)
	}
	if err := refuseChannelChatRouteHistoryRollbackWith(ctx, conn); err == nil || !strings.Contains(err.Error(), "cannot roll back channel chat routes") {
		t.Fatalf("rollback error = %v, want explicit channel Chat refusal", err)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM channel_chat_session_binding`); err != nil {
		t.Fatalf("clear route history: %v", err)
	}
	if err := refuseChannelChatRouteHistoryRollbackWith(ctx, conn); err != nil {
		t.Fatalf("guard without route history: %v", err)
	}
	if _, err := conn.Exec(ctx, `DROP INDEX CONCURRENTLY idx_channel_outbound_message_binding_route`); err != nil {
		t.Fatalf("drop index after guard: %v", err)
	}
}
