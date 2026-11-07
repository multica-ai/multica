CREATE TABLE channel_issue_delivery (
    issue_id            UUID PRIMARY KEY,
    channel_type        TEXT NOT NULL,
    installation_id     UUID NOT NULL,
    binding_id          UUID NOT NULL,
    channel_chat_id     TEXT NOT NULL,
    chat_type           TEXT NOT NULL CHECK (chat_type IN ('p2p', 'group')),
    channel_message_id  TEXT,
    channel_thread_id   TEXT,
    channel_sender_id    TEXT,
    route_revision      BIGINT NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE channel_issue_comment_notification (
    comment_id  UUID PRIMARY KEY,
    issue_id    UUID NOT NULL,
    claimed_at  TIMESTAMPTZ,
    sent_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
