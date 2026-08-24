ALTER TABLE channel_chat_context_generation
    ADD COLUMN IF NOT EXISTS disable_owner_connected_apps BOOLEAN NOT NULL DEFAULT FALSE;
