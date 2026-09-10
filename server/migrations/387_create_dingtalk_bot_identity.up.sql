-- Bot identity belongs to one DingTalk installation, independently of how many
-- groups have been observed or forgotten. Relationships are enforced and
-- cleaned up by application transactions; no foreign keys are added.
CREATE TABLE dingtalk_bot_identity (
    workspace_id       UUID NOT NULL,
    installation_id    UUID NOT NULL,
    bot_name            TEXT NOT NULL DEFAULT '',
    bot_identity_issue  TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
