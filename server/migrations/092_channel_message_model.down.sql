DROP INDEX IF EXISTS idx_channel_turn_waiting_task;
DROP INDEX IF EXISTS idx_channel_turn_status;
DROP INDEX IF EXISTS idx_channel_turn_conversation_created;
DROP INDEX IF EXISTS idx_channel_turn_inbound_message;
DROP INDEX IF EXISTS idx_channel_turn_inbound_event;
DROP TABLE IF EXISTS channel_turn;

DROP INDEX IF EXISTS idx_channel_message_entity_ref_workspace_key;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_entity_id;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_message;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_unique_key;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_unique_id;
DROP TABLE IF EXISTS channel_message_entity_ref;

DROP INDEX IF EXISTS idx_channel_message_reply_platform;
DROP INDEX IF EXISTS idx_channel_message_handoff;
DROP INDEX IF EXISTS idx_channel_message_thread_occurred;
DROP INDEX IF EXISTS idx_channel_message_conversation_occurred;
DROP INDEX IF EXISTS idx_channel_message_outbound_notification;
DROP INDEX IF EXISTS idx_channel_message_inbound_event;
DROP INDEX IF EXISTS idx_channel_message_event;
DROP INDEX IF EXISTS idx_channel_message_platform_message;
DROP TABLE IF EXISTS channel_message;

ALTER TABLE channel_outbound_notification
    DROP COLUMN IF EXISTS source_comment_id,
    DROP COLUMN IF EXISTS actor_id,
    DROP COLUMN IF EXISTS actor_type;

DROP INDEX IF EXISTS idx_channel_conversation_last_message;
DROP INDEX IF EXISTS idx_channel_conversation_workspace;
DROP INDEX IF EXISTS idx_channel_conversation_connection_chat;

ALTER TABLE channel_conversation RENAME TO channel_conversation_main;

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

INSERT INTO channel_conversation (
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    sender_external_id,
    active_event_id,
    active_since,
    created_at,
    updated_at
)
SELECT
    l.provider,
    l.connection_id,
    l.processing_key,
    COALESCE(e.chat_id, c.chat_id, '') AS chat_id,
    COALESCE(e.chat_type, c.chat_type, 'group') AS chat_type,
    COALESCE(e.sender_external_id, c.sender_external_id, '') AS sender_external_id,
    l.active_event_id,
    l.active_since,
    l.created_at,
    l.updated_at
FROM channel_processing_lock l
LEFT JOIN LATERAL (
    SELECT
        chat_id,
        chat_type,
        sender_external_id
    FROM channel_inbound_event e
    WHERE e.connection_id = l.connection_id
      AND e.conversation_key = l.processing_key
    ORDER BY e.created_at DESC
    LIMIT 1
) e ON true
LEFT JOIN LATERAL (
    SELECT
        chat_id,
        chat_type,
        sender_external_id
    FROM channel_conversation_main c
    WHERE c.connection_id = l.connection_id
    ORDER BY c.updated_at DESC
    LIMIT 1
) c ON true
ON CONFLICT (connection_id, conversation_key) DO UPDATE SET
    active_event_id = EXCLUDED.active_event_id,
    active_since = EXCLUDED.active_since,
    updated_at = EXCLUDED.updated_at;

INSERT INTO channel_conversation (
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    sender_external_id,
    active_event_id,
    active_since,
    created_at,
    updated_at
)
SELECT
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    sender_external_id,
    NULL AS active_event_id,
    NULL AS active_since,
    created_at,
    updated_at
FROM channel_conversation_main
ON CONFLICT (connection_id, conversation_key) DO NOTHING;

DROP TABLE channel_conversation_main;

DROP INDEX IF EXISTS idx_channel_processing_lock_stale;
DROP INDEX IF EXISTS idx_channel_processing_lock_active;
DROP TABLE IF EXISTS channel_processing_lock;
