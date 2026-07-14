-- Lark inbox notification delivery records.
--
-- Notification delivery uses a workspace-level Lark Bot rather than an
-- agent-bound chat Bot. Keep it in lark_installation so the existing WS hub,
-- lease, binding-token, and inbound dispatcher infrastructure can receive
-- replies from the notification app too; agent_id is only meaningful for
-- installation_kind='agent'.
ALTER TABLE lark_installation
    ADD COLUMN installation_kind TEXT NOT NULL DEFAULT 'agent'
        CHECK (installation_kind IN ('agent', 'notification'));

ALTER TABLE lark_installation
    ALTER COLUMN agent_id DROP NOT NULL;

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_kind_target_check
    CHECK (
        (installation_kind = 'agent' AND agent_id IS NOT NULL)
        OR (installation_kind = 'notification' AND agent_id IS NULL)
    );

CREATE UNIQUE INDEX uniq_lark_notification_installation_workspace
    ON lark_installation(workspace_id)
    WHERE installation_kind = 'notification';

-- A notification is sent as a direct message from the workspace notification
-- Bot to a bound Multica member. We persist the outbound Lark message id so
-- a later Lark quote-reply (`parent_id`) can be routed back to the original
-- Multica issue as a member comment instead of being ingested as ordinary
-- Agent chat.
CREATE TABLE lark_inbox_notification (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id   UUID NOT NULL REFERENCES lark_installation(id) ON DELETE CASCADE,
    inbox_item_id     UUID NOT NULL REFERENCES inbox_item(id) ON DELETE CASCADE,
    issue_id          UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    recipient_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    lark_open_id      TEXT NOT NULL,
    lark_message_id   TEXT NOT NULL,
    replied_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    delivered_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    replied_at        TIMESTAMPTZ,
    UNIQUE (inbox_item_id),
    UNIQUE (installation_id, lark_message_id)
);

CREATE INDEX idx_lark_inbox_notification_recipient
    ON lark_inbox_notification(workspace_id, recipient_user_id, delivered_at DESC);
