-- Immutable provenance for channel-ingested (Feishu/Slack) user messages.
-- The cancel draft-restore path gates on this instead of the deletable
-- channel_chat_session_binding row: archiving a session or rebinding an
-- installation deletes the binding but must never expose the original
-- inbound messages to restore-deletion.
ALTER TABLE chat_message
    ADD COLUMN channel_ingested BOOLEAN NOT NULL DEFAULT FALSE;
