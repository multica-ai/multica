ALTER TABLE channel_chat_session_binding
    ADD COLUMN IF NOT EXISTS context_revision BIGINT NOT NULL DEFAULT 1;

ALTER TABLE chat_message
    ADD COLUMN IF NOT EXISTS channel_context_revision BIGINT;

ALTER TABLE chat_message
    ADD COLUMN IF NOT EXISTS channel_outbound_type TEXT;

ALTER TABLE chat_message
    ADD COLUMN IF NOT EXISTS channel_outbound_installation_id UUID;

ALTER TABLE chat_message
    ADD COLUMN IF NOT EXISTS channel_outbound_chat_id TEXT;

ALTER TABLE chat_message
    ADD COLUMN IF NOT EXISTS channel_outbound_message_ids TEXT[];

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS channel_context_revision BIGINT;

CREATE TABLE IF NOT EXISTS channel_chat_context_generation (
    chat_session_id          UUID NOT NULL,
    revision                 BIGINT NOT NULL,
    history_start_message_id TEXT,
    history_end_message_id   TEXT,
    history_boundary_pending BOOLEAN NOT NULL DEFAULT FALSE,
    pending_fresh            BOOLEAN NOT NULL DEFAULT FALSE,
    initiator_user_id        UUID,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO channel_chat_context_generation (chat_session_id, revision)
SELECT chat_session_id, context_revision
FROM channel_chat_session_binding AS binding
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_chat_context_generation AS generation
    WHERE generation.chat_session_id = binding.chat_session_id
      AND generation.revision = binding.context_revision
);
