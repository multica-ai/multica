CREATE TABLE channel_conversation_context (
    connection_id       TEXT        NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    workspace_id        TEXT        NOT NULL DEFAULT '',
    chat_id             TEXT        NOT NULL,
    sender_external_id  TEXT        NOT NULL,
    thread_id           TEXT        NOT NULL DEFAULT '',
    entities            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, workspace_id, chat_id, sender_external_id, thread_id)
);

CREATE INDEX idx_channel_conversation_context_expiry
    ON channel_conversation_context (expires_at);
