-- Channel Chat route migration.
ALTER TABLE channel_chat_session_binding
    ADD COLUMN route_revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN retired_at TIMESTAMPTZ,
    ADD COLUMN history_start_message_id TEXT,
    ADD COLUMN history_end_message_id TEXT,
    ADD COLUMN history_boundary_pending BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE chat_session
    ADD COLUMN explicitly_created_at TIMESTAMPTZ;

CREATE TABLE channel_task_delivery (
    task_id             UUID NOT NULL,
    binding_id          UUID NOT NULL,
    installation_id     UUID NOT NULL,
    channel_type        TEXT NOT NULL,
    channel_chat_id     TEXT NOT NULL,
    chat_type           TEXT NOT NULL,
    channel_message_id  TEXT,
    channel_thread_id   TEXT,
    route_revision      BIGINT NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
