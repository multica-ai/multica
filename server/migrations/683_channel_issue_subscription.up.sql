-- IM chats (Feishu groups, Slack channels, ...) that receive an issue's
-- updates through the workspace's bot. Member-level subscriptions live in
-- issue_subscriber; this table addresses a chat, not a person.
--
-- thread_id / anchor_message_id are empty unless the subscription was made
-- inside a platform thread or topic, in which case updates go back there.
--
-- No foreign keys (repo rule): issue deletion and workspace teardown delete
-- these rows in their own statements; rows whose installation disappeared
-- are removed by the delivery path when it finds the installation gone.
CREATE TABLE IF NOT EXISTS channel_issue_subscription (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID        NOT NULL,
    issue_id          UUID        NOT NULL,
    installation_id   UUID        NOT NULL,
    channel_type      TEXT        NOT NULL,
    chat_id           TEXT        NOT NULL,
    thread_id         TEXT        NOT NULL DEFAULT '',
    anchor_message_id TEXT        NOT NULL DEFAULT '',
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
