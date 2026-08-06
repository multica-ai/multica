CREATE TABLE channel_outbound_queue (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID NOT NULL,
    workspace_id      UUID NOT NULL,
    channel_type      TEXT NOT NULL,
    chat_session_id   UUID,
    source_kind       TEXT NOT NULL,
    source_id         TEXT NOT NULL,
    target_chat_id    TEXT NOT NULL,
    target_chat_type  SMALLINT NOT NULL,
    msg_type          TEXT NOT NULL,
    payload_version   SMALLINT NOT NULL DEFAULT 1,
    payload           JSONB NOT NULL DEFAULT '{}'::jsonb,
    status            TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'sent', 'failed')),
    attempts          INT NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token       TEXT,
    lease_expires_at  TIMESTAMPTZ,
    sent_at           TIMESTAMPTZ,
    last_error        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
