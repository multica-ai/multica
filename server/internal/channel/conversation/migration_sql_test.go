// Package conversation owns the channel conversation, message, entity reference,
// and turn persistence boundary.
package conversation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigration092DDL(t *testing.T) {
	dbURL := os.Getenv("CHANNEL_MIGRATION_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set CHANNEL_MIGRATION_TEST_DATABASE_URL to validate migration 092 DDL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect migration test database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin migration ddl transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	schema := fmt.Sprintf("channel_migration_check_%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create migration check schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set migration check search_path: %v", err)
	}
	if _, err := tx.Exec(ctx, migration092Prerequisites); err != nil {
		t.Fatalf("create migration prerequisites: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_connection(id) VALUES ('conn-1');
INSERT INTO channel_conversation (
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    sender_external_id,
    active_event_id,
    active_since
) VALUES (
    'feishu',
    'conn-1',
    'conn-1:group:chat-1:user-1',
    'chat-1',
    'group',
    'user-1',
    NULL,
    NULL
);
`); err != nil {
		t.Fatalf("seed migration prerequisites: %v", err)
	}
	upSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "092_channel_message_model.up.sql"))
	if err != nil {
		t.Fatalf("read migration 092 up: %v", err)
	}
	if _, err := tx.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("run migration 092 up: %v", err)
	}
	downSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "092_channel_message_model.down.sql"))
	if err != nil {
		t.Fatalf("read migration 092 down: %v", err)
	}
	if _, err := tx.Exec(ctx, string(downSQL)); err != nil {
		t.Fatalf("run migration 092 down: %v", err)
	}
}

const migration092Prerequisites = `
CREATE TABLE channel_connection (
    id TEXT PRIMARY KEY
);

CREATE TABLE workspace (
    id UUID PRIMARY KEY
);

CREATE TABLE "user" (
    id UUID PRIMARY KEY
);

CREATE TABLE project (
    id UUID PRIMARY KEY
);

CREATE TABLE issue (
    id UUID PRIMARY KEY
);

CREATE TABLE inbox_item (
    id UUID PRIMARY KEY
);

CREATE TABLE agent (
    id UUID PRIMARY KEY
);

CREATE TABLE agent_task_queue (
    id UUID PRIMARY KEY
);

CREATE TABLE channel_inbound_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id TEXT NOT NULL,
    conversation_key TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    chat_type TEXT NOT NULL CHECK (chat_type IN ('group', 'direct')),
    sender_external_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE channel_outbound_notification (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE channel_conversation (
    provider            TEXT         NOT NULL,
    connection_id       TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    conversation_key    TEXT         NOT NULL,
    chat_id             TEXT         NOT NULL,
    chat_type           TEXT         NOT NULL CHECK (chat_type IN ('group', 'direct')),
    sender_external_id  TEXT         NOT NULL,
    active_event_id     UUID,
    active_since        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, conversation_key)
);
`
