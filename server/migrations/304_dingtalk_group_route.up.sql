-- A DingTalk Stream robot can participate in multiple group conversations.
-- Keep the robot credentials on channel_installation and route each observed
-- group to its own Multica agent. Relationships are application-owned; this
-- table intentionally has no foreign keys or inline unique constraints.
CREATE TABLE dingtalk_group_route (
    id                   UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    installation_id      UUID NOT NULL,
    conversation_id      TEXT NOT NULL,
    conversation_title   TEXT NOT NULL DEFAULT '',
    agent_id             UUID NOT NULL,
    revision             BIGINT NOT NULL DEFAULT 1,
    discovered_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
