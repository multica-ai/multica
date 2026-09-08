-- IM review push: the reply-attribution ledger.
--
-- channel_outbound_message cannot be reused: its binding_id and
-- route_revision are NOT NULL chat-session-route concepts, and an inbox
-- push has neither. Widening them to nullable would blur a table whose
-- identity is "an outbound message belonging to a chat route".
CREATE TABLE channel_push_message (
    installation_id    UUID NOT NULL,
    channel_type       TEXT NOT NULL,
    channel_message_id TEXT NOT NULL,
    workspace_id       UUID NOT NULL,
    recipient_user_id  UUID NOT NULL,
    issue_id           UUID,
    inbox_item_id      UUID NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
