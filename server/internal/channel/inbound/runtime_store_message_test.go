package inbound

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestDBInboundEventStore_AcceptEventCreatesChannelMessage(t *testing.T) {
	dbURL := os.Getenv("CHANNEL_MIGRATION_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set CHANNEL_MIGRATION_TEST_DATABASE_URL to validate inbound channel_message persistence")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("channel_inbound_message_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse scoped database url: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create scoped pool: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, runtimeStoreMessagePrerequisites); err != nil {
		t.Fatalf("create prerequisites: %v", err)
	}
	upSQL, err := os.ReadFile(migrationPath(t, "093_channel_message_model.up.sql"))
	if err != nil {
		t.Fatalf("read migration 093 up: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("apply migration 093: %v", err)
	}

	connID := "conn-message"
	if _, err := pool.Exec(ctx, `INSERT INTO channel_connection(id) VALUES ($1)`, connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace DEFAULT VALUES RETURNING id::text`).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	store := NewDBInboundEventStore(pool)
	first := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: connID,
		EventID:             "evt-1",
		Type:                port.EventTypeMessageReceived,
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "user-1",
		SenderName:          "User One",
		MessageID:           "msg-1",
		Text:                "先看这个",
	}
	firstResult, err := store.AcceptEvent(ctx, first, AcceptOptions{})
	if err != nil {
		t.Fatalf("accept first event: %v", err)
	}
	if !firstResult.Accepted {
		t.Fatalf("first event was not accepted: %+v", firstResult)
	}

	var firstMessageID, firstConversationKey, firstProcessingKey string
	if err := pool.QueryRow(ctx, `
SELECT m.id::text, c.conversation_key, e.conversation_key
FROM channel_message m
JOIN channel_conversation c ON c.id = m.conversation_id
JOIN channel_inbound_event e ON e.id = m.inbound_event_id
WHERE m.event_id = $1
`, first.EventID).Scan(&firstMessageID, &firstConversationKey, &firstProcessingKey); err != nil {
		t.Fatalf("load first channel message: %v", err)
	}
	if firstConversationKey != connID+":group:chat-1" {
		t.Fatalf("conversation_key = %q, want chat-scoped key", firstConversationKey)
	}
	if firstProcessingKey != connID+":group:chat-1:user-1" {
		t.Fatalf("processing key = %q, want sender-scoped key", firstProcessingKey)
	}

	second := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: connID,
		EventID:             "evt-2",
		Type:                port.EventTypeMessageReceived,
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "user-2",
		SenderName:          "User Two",
		MessageID:           "msg-2",
		Text:                "同意",
		ReplyToMessageID:    "msg-1",
		QuotedMessageID:     "msg-1",
		QuotedText:          "先看这个",
		ThreadID:            "thread-1",
	}
	secondResult, err := store.AcceptEvent(ctx, second, AcceptOptions{})
	if err != nil {
		t.Fatalf("accept second event: %v", err)
	}
	if !secondResult.Accepted {
		t.Fatalf("second event was not accepted: %+v", secondResult)
	}

	var replyToID, quotedID, replyPlatformID, quotedPlatformID, threadID string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(reply_to_message_id::text, ''),
       COALESCE(quoted_message_id::text, ''),
       reply_to_platform_message_id,
       quoted_platform_message_id,
       thread_id
FROM channel_message
WHERE event_id = $1
`, second.EventID).Scan(&replyToID, &quotedID, &replyPlatformID, &quotedPlatformID, &threadID); err != nil {
		t.Fatalf("load second channel message: %v", err)
	}
	if replyToID != firstMessageID || quotedID != firstMessageID {
		t.Fatalf("resolved refs = (%q, %q), want first message %q", replyToID, quotedID, firstMessageID)
	}
	if replyPlatformID != "msg-1" || quotedPlatformID != "msg-1" {
		t.Fatalf("platform refs = (%q, %q), want msg-1", replyPlatformID, quotedPlatformID)
	}
	if threadID != "thread-1" {
		t.Fatalf("thread_id = %q, want thread-1", threadID)
	}

	if err := store.SaveEvent(ctx, secondResult.EventID, second, InboundPhaseIntent, ChatBindingContext{WorkspaceID: workspaceID}); err != nil {
		t.Fatalf("save second event with workspace: %v", err)
	}
	var messageWorkspaceID string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(workspace_id::text, '')
FROM channel_message
WHERE event_id = $1
`, second.EventID).Scan(&messageWorkspaceID); err != nil {
		t.Fatalf("load message workspace: %v", err)
	}
	if messageWorkspaceID != workspaceID {
		t.Fatalf("workspace_id = %q, want %q", messageWorkspaceID, workspaceID)
	}
	var conversationWorkspaceID string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(c.workspace_id::text, '')
FROM channel_conversation c
JOIN channel_message m ON m.conversation_id = c.id
WHERE m.event_id = $1
`, second.EventID).Scan(&conversationWorkspaceID); err != nil {
		t.Fatalf("load conversation workspace: %v", err)
	}
	if conversationWorkspaceID != workspaceID {
		t.Fatalf("conversation workspace_id = %q, want %q", conversationWorkspaceID, workspaceID)
	}
}

func migrationPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve current test path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", name)
}

const runtimeStoreMessagePrerequisites = `
CREATE TABLE channel_connection (
    id TEXT PRIMARY KEY
);

CREATE TABLE workspace (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE "user" (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE project (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE issue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE inbox_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE agent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE agent_task_queue (
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

CREATE TABLE channel_inbound_event (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider              TEXT         NOT NULL,
    connection_id         TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    event_id              TEXT         NOT NULL,
    event_type            TEXT         NOT NULL,
    conversation_key      TEXT         NOT NULL,
    chat_id               TEXT         NOT NULL,
    chat_type             TEXT         NOT NULL CHECK (chat_type IN ('group', 'direct')),
    sender_external_id    TEXT         NOT NULL,
    sender_name           TEXT         NOT NULL DEFAULT '',
    message_id            TEXT         NOT NULL DEFAULT '',
    text                  TEXT         NOT NULL DEFAULT '',
    canonical_event       JSONB        NOT NULL,
    raw_payload           JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status                TEXT         NOT NULL DEFAULT 'queued'
                                        CHECK (status IN (
                                            'queued',
                                            'processing',
                                            'processed',
                                            'waiting_agent',
                                            'waiting_user',
                                            'failed',
                                            'dead',
                                            'rejected_backpressure'
                                        )),
    phase                 TEXT         NOT NULL DEFAULT 'pre'
                                        CHECK (phase IN ('pre', 'intent', 'post', 'done')),
    wait_kind             TEXT         CHECK (wait_kind IN ('intent', 'action', 'channel_turn', 'user_clarification')),
    wait_task_id          UUID         REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    wait_expires_at       TIMESTAMPTZ,
    workspace_id          UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    default_project_id    UUID         REFERENCES project(id) ON DELETE SET NULL,
    intent_payload        JSONB,
    dispatch_completed_at TIMESTAMPTZ,
    dispatch_reply_text   TEXT         NOT NULL DEFAULT '',
    attempts              INTEGER      NOT NULL DEFAULT 0,
    max_attempts          INTEGER      NOT NULL DEFAULT 3,
    next_attempt_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    locked_at             TIMESTAMPTZ,
    locked_by             TEXT,
    started_at            TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    last_error            TEXT,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (connection_id, event_id)
);

CREATE TABLE channel_outbound_notification (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);
`
