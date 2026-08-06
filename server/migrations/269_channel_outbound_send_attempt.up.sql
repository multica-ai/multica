CREATE TABLE channel_outbound_send_attempt (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id           UUID NOT NULL,
    installation_id    UUID NOT NULL,
    workspace_id       UUID NOT NULL,
    chat_session_id    UUID,
    target_chat_id     TEXT NOT NULL,
    target_chat_type   SMALLINT NOT NULL,
    attempted_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
