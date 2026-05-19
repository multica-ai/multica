// Package conversation owns the channel conversation, message, entity
// reference, and turn persistence boundary.
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

func TestFindLatestCompletedTurnWithInboundMessageJoin(t *testing.T) {
	dbURL := os.Getenv("CHANNEL_MIGRATION_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set CHANNEL_MIGRATION_TEST_DATABASE_URL to validate channel turn lookup")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect migration test database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin channel turn lookup transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	schema := fmt.Sprintf("channel_turn_lookup_%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create channel turn lookup schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set channel turn lookup search_path: %v", err)
	}
	if _, err := tx.Exec(ctx, channelMigrationPrerequisites); err != nil {
		t.Fatalf("create migration prerequisites: %v", err)
	}
	upSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "090_channel_integration.up.sql"))
	if err != nil {
		t.Fatalf("read channel migration up: %v", err)
	}
	if _, err := tx.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("run channel migration up: %v", err)
	}

	const (
		workspaceID    = "550e8400-e29b-41d4-a716-446655440001"
		conversationID = "550e8400-e29b-41d4-a716-446655440002"
		messageID      = "550e8400-e29b-41d4-a716-446655440003"
		turnID         = "550e8400-e29b-41d4-a716-446655440004"
		connectionID   = "conn-turn-lookup"
		senderID       = "ou_sender"
		threadID       = "thread-1"
	)

	if _, err := tx.Exec(ctx, `INSERT INTO workspace (id) VALUES ($1::uuid)`, workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_connection (id, provider, display_name)
VALUES ($1, 'feishu', 'Feishu')
`, connectionID); err != nil {
		t.Fatalf("insert channel connection: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_conversation (
    id, provider, connection_id, conversation_key, chat_id, chat_type,
    conversation_type, workspace_id, sender_external_id
) VALUES (
    $1::uuid, 'feishu', $2, 'conv-key', 'oc_1', 'group',
    'thread', $3::uuid, $4
)
`, conversationID, connectionID, workspaceID, senderID); err != nil {
		t.Fatalf("insert channel conversation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_message (
    id, provider, connection_id, conversation_id, workspace_id, chat_id,
    chat_type, thread_id, direction, message_type, sender_type,
    sender_external_id, text
) VALUES (
    $1::uuid, 'feishu', $2, $3::uuid, $4::uuid, 'oc_1',
    'group', $5, 'inbound', 'user', 'user',
    $6, 'hello'
)
`, messageID, connectionID, conversationID, workspaceID, threadID, senderID); err != nil {
		t.Fatalf("insert channel message: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_turn (
    id, provider, connection_id, conversation_id, workspace_id,
    inbound_message_id, sender_external_id, status, completed_at,
    result_payload
) VALUES (
    $1::uuid, 'feishu', $2, $3::uuid, $4::uuid,
    $5::uuid, $6, 'completed', now(),
    '{"pending_action":{"kind":"confirm","expires_at":"2099-01-01T00:00:00Z"}}'::jsonb
)
`, turnID, connectionID, conversationID, workspaceID, messageID, senderID); err != nil {
		t.Fatalf("insert channel turn: %v", err)
	}

	store := NewTxStore(tx)
	got, ok, err := store.FindLatestCompletedTurn(ctx, connectionID, conversationID, senderID, threadID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("FindLatestCompletedTurn: %v", err)
	}
	if !ok {
		t.Fatal("FindLatestCompletedTurn ok = false, want true")
	}
	if got.ID != turnID {
		t.Fatalf("turn id = %q, want %q", got.ID, turnID)
	}
	if got.InboundMessageID != messageID {
		t.Fatalf("inbound message id = %q, want %q", got.InboundMessageID, messageID)
	}
}
