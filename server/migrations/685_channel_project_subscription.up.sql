-- IM chats that receive updates for every issue in a project through the
-- workspace's bot: new issues, status and assignee changes, and comments.
-- The issue-level counterpart is channel_issue_subscription; a chat that
-- follows both an issue and its project gets each update once.
--
-- No foreign keys (repo rule): project deletion and workspace teardown
-- delete these rows; rows whose installation disappeared are removed by the
-- delivery path when it finds the installation gone.
CREATE TABLE IF NOT EXISTS channel_project_subscription (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID        NOT NULL,
    project_id        UUID        NOT NULL,
    installation_id   UUID        NOT NULL,
    channel_type      TEXT        NOT NULL,
    chat_id           TEXT        NOT NULL,
    thread_id         TEXT        NOT NULL DEFAULT '',
    anchor_message_id TEXT        NOT NULL DEFAULT '',
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
