-- Product-facing DingTalk group inventory. This table records observations and
-- activity only: it deliberately has no agent routing columns or foreign keys.
-- Relationships are enforced and cleaned up by application transactions.
CREATE TABLE dingtalk_group_presence (
    workspace_id         UUID NOT NULL,
    installation_id      UUID NOT NULL,
    conversation_id      TEXT NOT NULL,
    conversation_title   TEXT NOT NULL DEFAULT '',
    bot_name              TEXT NOT NULL DEFAULT '',
    bot_identity_issue    TEXT NOT NULL DEFAULT '',
    first_seen_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at        TIMESTAMPTZ,
    mention_count         BIGINT NOT NULL DEFAULT 0 CHECK (mention_count >= 0),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
