-- Channel Chat route migration.
CREATE TABLE channel_outbound_message (
    installation_id   UUID NOT NULL,
    channel_type      TEXT NOT NULL,
    channel_message_id TEXT NOT NULL,
    binding_id        UUID NOT NULL,
    route_revision    BIGINT NOT NULL,
    task_id           UUID,
    outbound_kind     TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
