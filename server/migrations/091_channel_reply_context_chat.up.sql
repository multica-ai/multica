ALTER TABLE channel_reply_context
    ADD COLUMN IF NOT EXISTS chat_id TEXT NOT NULL DEFAULT '',
    DROP CONSTRAINT IF EXISTS channel_reply_context_pkey,
    ADD PRIMARY KEY (connection_id, external_user_id, chat_id);
CREATE INDEX IF NOT EXISTS idx_channel_reply_context_chat
    ON channel_reply_context(connection_id, external_user_id, chat_id);
