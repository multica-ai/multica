CREATE TABLE dingtalk_direct_route (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    connector_id    UUID NOT NULL,
    channel_user_id TEXT NOT NULL,
    channel_chat_id TEXT NOT NULL DEFAULT '',
    workspace_id    UUID NOT NULL,
    agent_id        UUID NOT NULL,
    revision        BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
